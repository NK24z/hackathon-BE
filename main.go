package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/oklog/ulid"

	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type UserResForHTTPGet struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Mail string `json:"email"`
}

type Reply struct {
	Id       string `json:"id"`
	Content  string `json:"content"`
	UserName string `json:"user_id"`
}

type Like struct {
	Id       string `json:"id"`
	PostId   string `json:"post_id"`
	UserName string `json:"user_id"`
}

type PostWithRepliesAndLikes struct {
	Id        string  `json:"id"`
	Content   string  `json:"content"`
	UserName  string  `json:"user_id"`
	Replies   []Reply `json:"replies"`
	Likes     []Like  `json:"likes"`
	LikeCount int     `json:"like_count"`
}

type Post struct {
	Id       string `json:"id"`
	Content  string `json:"content"`
	UserName string `json:"user_id"`
}

var db *sql.DB

func init() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/Users/kokuryo natsumi/GolandProjects/awesomeProject2/term6-kokuryo-natsumi-f5b525f76ac5.json")

	mysqlUser := os.Getenv("MYSQL_USER")
	mysqlPassword := os.Getenv("MYSQL_PWD")
	mysqlDatabase := os.Getenv("MYSQL_DATABASE")

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s", mysqlUser, mysqlPassword, "127.0.0.1", mysqlDatabase)

	_db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	if err := _db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	db = _db
}

func handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch r.Method {
	case http.MethodGet:

		rows, err := db.Query("SELECT id, name FROM userEx")
		if err != nil {
			log.Printf("fail: db.Query, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		users := make([]UserResForHTTPGet, 0)
		for rows.Next() {
			var u UserResForHTTPGet
			if err := rows.Scan(&u.Id, &u.Name, &u.Mail); err != nil {
				log.Printf("fail: rows.Scan, %v\n", err)
				if err := rows.Close(); err != nil {
					log.Printf("fail: rows.Close(), %v\n", err)
				}
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			users = append(users, u)
		}

		bytes, err := json.Marshal(users)
		if err != nil {
			log.Printf("fail: json.Marshal, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(bytes)

	case http.MethodPost:
		var u UserResForHTTPGet
		if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
			log.Printf("fail: json.NewDecoder.Decode, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		t := time.Now().UTC()
		entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
		u.Id = ulid.MustNew(ulid.Timestamp(t), entropy).String()

		tx, err := db.Begin()
		if err != nil {
			log.Printf("fail: db.Begin, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec("INSERT INTO userEx (id, name, mail) VALUES (?, ?, ?)", u.Id, u.Name, u.Mail)
		if err != nil {
			tx.Rollback() // エラーが発生した場合ロールバック
			log.Printf("fail: tx.Exec, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("fail: tx.Commit, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": u.Id})

	default:
		log.Printf("fail: HTTP Method is %s\n", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func postHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// 投稿データ、ユーザー名、いいねの個数を取得
		rows, err := db.Query(`
            SELECT p.id, p.content, u.name, COUNT(l.id) AS like_count
            FROM posts p
            JOIN userEx u ON p.user_id = u.id
            LEFT JOIN likes l ON p.id = l.post_id
            GROUP BY p.id, u.name
        `)
		if err != nil {
			log.Printf("fail: db.Query(posts), %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		postsWithRepliesAndLikes := make([]PostWithRepliesAndLikes, 0)

		for rows.Next() {
			var post PostWithRepliesAndLikes
			if err := rows.Scan(&post.Id, &post.Content, &post.UserName, &post.LikeCount); err != nil {
				log.Printf("fail: rows.Scan(posts), %v\n", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// 返信データとユーザー名を取得
			replyRows, err := db.Query(`
                SELECT r.id, r.content, u.name
                FROM replies r
                JOIN userEx u ON r.user_id = u.id
                WHERE r.post_id = ?
            `, post.Id)
			if err != nil {
				log.Printf("fail: db.Query(replies), %v\n", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer replyRows.Close()

			replies := make([]Reply, 0)
			for replyRows.Next() {
				var reply Reply
				if err := replyRows.Scan(&reply.Id, &reply.Content, &reply.UserName); err != nil {
					log.Printf("fail: rows.Scan(replies), %v\n", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				replies = append(replies, reply)
			}

			post.Replies = replies

			// いいねデータとユーザー名を取得
			likeRows, err := db.Query(`
                SELECT l.id, u.name
                FROM likes l
                JOIN userEx u ON l.user_id = u.id
                WHERE l.post_id = ?
            `, post.Id)
			if err != nil {
				log.Printf("fail: db.Query(likes), %v\n", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			defer likeRows.Close()

			likes := make([]Like, 0)
			for likeRows.Next() {
				var like Like
				if err := likeRows.Scan(&like.Id, &like.UserName); err != nil {
					log.Printf("fail: rows.Scan(likes), %v\n", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				likes = append(likes, like)
			}

			post.Likes = likes
			postsWithRepliesAndLikes = append(postsWithRepliesAndLikes, post)
		}

		// JSONに変換して返す
		bytes, err := json.Marshal(postsWithRepliesAndLikes)
		if err != nil {
			log.Printf("fail: json.Marshal(postsWithRepliesAndLikes), %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(bytes)
	case http.MethodPost:
		var p Post // 新しいリソース構造体
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			log.Printf("fail: json.NewDecoder.Decode, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		t := time.Now().UTC()
		entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
		p.Id = ulid.MustNew(ulid.Timestamp(t), entropy).String()

		tx, err := db.Begin()
		if err != nil {
			log.Printf("fail: db.Begin, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec("INSERT INTO posts (id, user_id, content) VALUES (?, ?, ?)", p.Id, p.UserName, p.Content)
		if err != nil {
			tx.Rollback() // エラーが発生した場合ロールバック
			log.Printf("fail: tx.Exec, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("fail: tx.Commit, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		log.Printf("fail: HTTP Method is %s\n", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func likeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPost {
		var like Like
		// リクエストボディからデータをデコード
		if err := json.NewDecoder(r.Body).Decode(&like); err != nil {
			log.Printf("fail: json.NewDecoder.Decode, %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// いいねIDを生成
		t := time.Now().UTC()
		entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
		likeId := ulid.MustNew(ulid.Timestamp(t), entropy).String()

		// データベースにいいねを挿入
		tx, err := db.Begin()
		if err != nil {
			log.Printf("fail: db.Begin, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(`
            INSERT INTO likes (id, post_id, user_id)
            VALUES (?, ?, ?)
        `, likeId, like.PostId, like.UserName)
		if err != nil {
			tx.Rollback()
			log.Printf("fail: tx.Exec, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("fail: tx.Commit, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// 成功レスポンス
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "like added"})
	} else {
		log.Printf("fail: HTTP Method is %s\n", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func main() {
	http.HandleFunc("/user", handler)
	http.HandleFunc("/post", postHandler)
	http.HandleFunc("/like", likeHandler)
	closeDBWithSysCall()

	log.Println("Listening...")
	if err := http.ListenAndServe(":8000", nil); err != nil {
		log.Fatal(err)
	}
}

func closeDBWithSysCall() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sig
		log.Printf("received syscall, %v", s)

		if err := db.Close(); err != nil {
			log.Fatal(err)
		}
		log.Printf("success: db.Close()")
		os.Exit(0)
	}()
}
