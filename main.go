package main

import (
	"cloud.google.com/go/vertexai/genai"
	"context"
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
	Mail string `json:"mail"`
}

type Reply struct {
	Id      string `json:"id"`
	Content string `json:"content"`
	//UserName string `json:"user_id"`
}

type Like struct {
	PostId string `json:"id"`
}

type Post struct {
	Id       string `json:"id"`
	Content  string `json:"content"`
	UserName string `json:"user_id"`
}

type Request struct {
	Email string `json:"email"`
}

type Response struct {
	Username string `json:"username"`
	Error    string `json:"error,omitempty"`
}

const (
	location  = "us-central1"
	modelName = "gemini-1.5-flash-002"
	projectID = "term6-kokuryo-natsumi" // ① 自分のプロジェクトIDを指定する
)

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

		mail := r.URL.Query().Get("mail")
		log.Println(mail)
		if mail == "" {
			log.Println("fail: email is empty")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		rows, err := db.Query("SELECT id, name, mail FROM userEx WHERE mail = ?", mail)
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
		// 投稿データといいねの個数を取得
		rows, err := db.Query(`
			SELECT p.id, p.content, COUNT(l.id) AS like_count
			FROM posts p
			LEFT JOIN likes l ON p.id = l.post_id
			GROUP BY p.id, p.content
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
			if err := rows.Scan(&post.Id, &post.Content, &post.LikeCount); err != nil {
				log.Printf("fail: rows.Scan(posts), %v\n", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			//返信データを取得
			replyRows, err := db.Query(`
				SELECT r.id, r.content
				FROM replies r
				WHERE r.parent_id = ?
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
				if err := replyRows.Scan(&reply.Id, &reply.Content); err != nil {
					log.Printf("fail: rows.Scan(replies), %v\n", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				replies = append(replies, reply)
			}
			post.Replies = replies

			// いいねデータを取得
			likeRows, err := db.Query(`
				SELECT l.id
				FROM likes l
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
				if err := likeRows.Scan(&like.PostId); err != nil {
					log.Printf("fail: rows.Scan(likes), %v\n", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				likes = append(likes, like)
			}
			post.Likes = likes

			postsWithRepliesAndLikes = append(postsWithRepliesAndLikes, post)
		}

		if len(postsWithRepliesAndLikes) == 0 {
			log.Println("No data found in posts query")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]")) // 空のJSONレスポンスを返す
			return
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
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 投稿内容をGeminiを使って添削
		correctedContent, err := generateCorrectedContent(p.Content)
		if err != nil {
			log.Printf("fail: generateCorrectedContent, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		str := fmt.Sprintf("%v", correctedContent)
		p.Content = str // 添削後の内容を反映

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

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("Post created successfully"))
	default:
		log.Printf("fail: HTTP Method is %s\n", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func generateCorrectedContent(content string) (genai.Part, error) {
	// Gemini APIを使って投稿内容を添削

	ctx := context.Background()
	client, err := genai.NewClient(ctx, projectID, location)
	if err != nil {
		return nil, fmt.Errorf("error creating client: %w", err)
	}

	gemini := client.GenerativeModel(modelName)
	prompt := genai.Text("短いものはそのままにし、長い場合は以下の文章を添削し、50文字以下にしてください:\n" + content)
	resp, err := gemini.GenerateContent(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("error generating content: %w", err)
	}
	text := resp.Candidates[0].Content.Parts[0]

	rb, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("json.MarshalIndent: %w", err)
	}
	fmt.Println(string(rb))

	return text, nil
}

func likeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
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

		// `post_id` のバリデーション
		if like.PostId == "" {
			log.Printf("fail: post_id is empty\n")
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// サーバー側でULIDを生成
		t := time.Now().UTC()
		entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
		generatedId := ulid.MustNew(ulid.Timestamp(t), entropy).String()

		// データベースに挿入
		tx, err := db.Begin()
		if err != nil {
			log.Printf("fail: db.Begin, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(`
            INSERT INTO likes (id, post_id)
            VALUES (?, ?)
        `, generatedId, like.PostId)
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
		json.NewEncoder(w).Encode(map[string]string{
			"status": "like added",
			"id":     generatedId, // 生成したULIDをレスポンスで返す
		})
	} else {
		log.Printf("fail: HTTP Method is %s\n", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func replyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "POST,GET,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method == http.MethodPost {
		var reply struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		}

		// リクエストボディからデータをデコード
		if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
			log.Printf("fail: json.NewDecoder.Decode, %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 返信IDを生成
		t := time.Now().UTC()
		entropy := ulid.Monotonic(rand.New(rand.NewSource(t.UnixNano())), 0)
		replyID := ulid.MustNew(ulid.Timestamp(t), entropy).String()

		// データベースに返信を挿入
		tx, err := db.Begin()
		if err != nil {
			log.Printf("fail: db.Begin, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		_, err = tx.Exec(`
            INSERT INTO replies (id, parent_id, content)
            VALUES (?, ?, ?)
        `, replyID, reply.ID, reply.Content)
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
		json.NewEncoder(w).Encode(map[string]string{"status": "reply added"})
	} else {
		log.Printf("fail: HTTP Method is %s\n", r.Method)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

func handlerGetName(w http.ResponseWriter, r *http.Request) {
	// CORSヘッダーの設定
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// OPTIONSリクエストの場合はヘッダーを返して終了
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// リクエストボディのデコード
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// データベースからユーザー名を取得
	username, err := getUsernameByEmail(db, req.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			json.NewEncoder(w).Encode(Response{Error: "User not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// レスポンスを返す
	json.NewEncoder(w).Encode(Response{Username: username})
}

func getUsernameByEmail(db *sql.DB, mail string) (string, error) {
	var username string
	query := "SELECT name FROM userEx WHERE mail = ?"
	if err := db.QueryRow(query, mail).Scan(&username); err != nil {
		return "", err
	}
	return username, nil
}

func main() {
	http.HandleFunc("/user", handler)
	http.HandleFunc("/post", postHandler)
	http.HandleFunc("/like", likeHandler)
	http.HandleFunc("/get-username", handlerGetName)
	http.HandleFunc("/get-mail", handler)
	http.HandleFunc("/comment", replyHandler)

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
