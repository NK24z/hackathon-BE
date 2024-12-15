FROM golang:1.21 AS builder
WORKDIR /  # アプリケーションの作業ディレクトリを設定（この場合、アプリケーションコードはルートにありますが、/app ディレクトリ内で作業します）
COPY ./go.mod ./go.sum ./
COPY ./main.go ./  # 必要な依存関係をコピー
RUN go mod download    # 依存関係をダウンロード
COPY . .               # 残りのソースコード（main.goを含む）をコピー
RUN go build -o main .  # バイナリを /app/main に生成

FROM debian:bullseye-slim
ENV PORT=8080
WORKDIR /app  # 実行時の作業ディレクトリを設定
COPY --from=builder /app/main .  # ビルドしたバイナリをコピー
EXPOSE 8080
CMD ["./main"]  # アプリケーションを実行
