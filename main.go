package main

import (
	"cloud.google.com/go/vertexai/genai"
	"context"
	_ "context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"io"
	_ "io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"syscall"

	_ "cloud.google.com/go/vertexai/genai"
)

const (
	location  = "asia-northeast1"
	modelName = "gemini-1.5-flash-002"
	projectID = "term6-haruto-kawano" // ① 自分のプロジェクトIDを指定する
)

type TweetsResForHTTPGet struct {
	Id int `json:"id"`
	//UserId    int    `json:"user_id"`
	Content string `json:"content"`
	//CreatedAt int    `json:"created_at"`
	LikesCount int    `json:"likes_count"`
	ParentID   *int   `json:"parent_id,omitempty"`
	FireRate   string `json:"fire_rate"`
}

// ① GoプログラムからMySQLへ接続
var db *sql.DB

func CORSMiddlewareProd(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		// プリフライトリクエストの応答
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// 次のミドルウェアまたはハンドラを呼び出す
		next.ServeHTTP(w, r)
	})
}

func init() {
	// ①-1
	err := godotenv.Load(".env")
	mysqlUser := os.Getenv("MySQL_USER")
	mysqlUserPwd := os.Getenv("MySQL_PWD")
	mysqlDatabase := os.Getenv("MySQL_DATABASE")
	mysqlHost := os.Getenv("MySQL_HOST")
	fmt.Printf("User: %s\n", mysqlUser)
	fmt.Printf("User: %s\n", mysqlUser)
	fmt.Printf("Password: %s\n", mysqlUserPwd)
	fmt.Printf("Database: %s\n", mysqlDatabase)
	fmt.Printf("mysqlHost: %s\n", mysqlHost)

	// ①-2
	_db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@%s/%s", mysqlUser, mysqlUserPwd, mysqlHost, mysqlDatabase))
	if err != nil {
		log.Fatalf("fail: sql.Open, %v\n", err)
	}
	fmt.Printf("done\n")
	// ①-3
	if err := _db.Ping(); err != nil {
		log.Fatalf("fail: _db.Ping, %v\n", err)
	}
	db = _db
	fmt.Printf("done\n")
}

func main() {
	// マルチプレクサを作成
	mux := http.NewServeMux()
	// CORSミドルウェアを適用
	wrappedMux := CORSMiddlewareProd(mux)
	// ハンドラを登録
	mux.HandleFunc("/tweet", handler)
	mux.HandleFunc("/tweet/like", likeHandler)
	mux.HandleFunc("/tweet/replies", fetchRepliesHandler)
	mux.HandleFunc("/tweet/reply", replyHandler)
	// Ctrl+CでHTTPサーバー停止時にDBをクローズする
	closeDBWithSysCall()

	// 8000番ポートでリクエストを待ち受ける
	log.Println("Listening...")
	if err := http.ListenAndServe(":8080", wrappedMux); err != nil {
		log.Fatal(err)
	}
}

// ③ Ctrl+CでHTTPサーバー停止時にDBをクローズする
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

func generateContentFromText(w io.Writer, projectID, promptText string) string {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, projectID, location)
	if err != nil {
		fmt.Printf("error creating client: %w", err)
	}

	// ③ Geminiにpromptを送る
	gemini := client.GenerativeModel(modelName)
	prompt := genai.Text(promptText)
	resp, _ := gemini.GenerateContent(ctx, prompt)
	// ④ 結果を画面に出力する
	rb, _ := json.MarshalIndent(resp, "", "  ")
	var result map[string]interface{}

	_ = json.Unmarshal(rb, &result)
	if err != nil {
		log.Fatalf("Error unmarshaling JSON: %v", err)
	}
	candidates := result["Candidates"].([]interface{})
	fmt.Println("done1")
	// candidates はスライスなのでインターフェース型に変換
	content := candidates[0].(map[string]interface{})["Content"].(map[string]interface{})
	fmt.Println("done2")
	parts := content["Parts"].([]interface{})
	fmt.Println("done3")
	var partsStr string
	for _, part := range parts {
		partsStr += part.(string) + " " // 文字列として結合
	}

	fmt.Println("Returned type:", reflect.TypeOf(resp))
	fmt.Printf("output: %s", partsStr)
	return partsStr
}

// ② /userでリクエストされたらnameパラメーターと一致する名前を持つレコードをJSON形式で返す
func handler(w http.ResponseWriter, r *http.Request) {
	// CORSヘッダーを設定
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")

	switch r.Method {
	case http.MethodPost:
		var tweet TweetsResForHTTPGet
		// リクエストボディからツイート内容をデコード
		if err := json.NewDecoder(r.Body).Decode(&tweet); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		// AIにプロンプトを送信してレスポンスを取得
		fmt.Printf("tweet_content: %s \n", tweet.Content)
		aiResponse := generateContentFromText(os.Stdout, projectID, string(tweet.Content)+"これまでの内容を投稿した際に炎上する確率を0%~100%で回答してください。5%、のように確率のみ答えてください")

		tweet.FireRate = aiResponse // Geminiの応答をFireRateとして格納
		// データベースにツイートを追加（fire_rateも含める）
		db.Exec("INSERT INTO tweets (content, fire_rate) VALUES (?, ?)", tweet.Content, tweet.FireRate)
		// レスポンスのヘッダーを設定
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tweet); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodGet:
		// データベースから全ツイートを取得
		rows, err := db.Query("SELECT id, content, likes_count, parent_id, COALESCE(fire_rate, '') AS fire_rate FROM tweets")
		if err != nil {
			log.Printf("fail: db.Query, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// 結果を格納するスライスを作成
		tweets := make([]TweetsResForHTTPGet, 0)
		for rows.Next() {
			var u TweetsResForHTTPGet

			// データをスキャンして構造体にマッピング
			if err := rows.Scan(&u.Id, &u.Content, &u.LikesCount, &u.ParentID, &u.FireRate); err != nil {
				log.Printf("fail: %v\n", err)
				http.Error(w, "データ読み込みエラー", http.StatusInternalServerError)
				return
			}

			tweets = append(tweets, u)
		}

		// JSONにエンコードしてレスポンスを送信
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tweets); err != nil {
			log.Printf("fail: json.Marshal, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}

func likeHandler(w http.ResponseWriter, r *http.Request) {
	// いいねを押したいツイートIDを取得
	tweetID := r.URL.Query().Get("id")
	if tweetID == "" {
		http.Error(w, "Missing tweet ID", http.StatusBadRequest)
		return
	}

	// ツイートをデータベースから取得
	var tweet TweetsResForHTTPGet
	err := db.QueryRow("SELECT id, content, likes_count FROM tweets WHERE id = ?", tweetID).Scan(&tweet.Id, &tweet.Content, &tweet.LikesCount)
	if err != nil {
		log.Printf("Error retrieving tweet: %v", err)
		http.Error(w, "Tweet not found", http.StatusNotFound)
		return
	}

	// いいね数をインクリメント
	tweet.LikesCount++
	_, err = db.Exec("UPDATE tweets SET likes_count = ? WHERE id = ?", tweet.LikesCount, tweet.Id)
	if err != nil {
		log.Printf("Error updating likes count: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// 更新後のいいね数をレスポンスとして返す
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tweet); err != nil {
		log.Printf("fail: json.Marshal, %v\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func replyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reply struct {
		Content  string `json:"content"`
		ParentID int    `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("INSERT INTO tweets (content, parent_id) VALUES (?, ?)", reply.Content, reply.ParentID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// リプライ取得処理
func fetchRepliesHandler(w http.ResponseWriter, r *http.Request) {
	tweetID := r.URL.Query().Get("id")
	if tweetID == "" {
		http.Error(w, "Missing tweet ID", http.StatusBadRequest)
		return
	}

	// 親ツイート取得
	var parentTweet TweetsResForHTTPGet
	err := db.QueryRow("SELECT id, content, likes_count FROM tweets WHERE id = ?", tweetID).Scan(&parentTweet.Id, &parentTweet.Content, &parentTweet.LikesCount)
	if err != nil {
		http.Error(w, "Parent tweet not found", http.StatusNotFound)
		return
	}

	// リプライ取得
	rows, err := db.Query("SELECT id, content, likes_count FROM tweets WHERE parent_id = ?", tweetID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	replies := []TweetsResForHTTPGet{}
	for rows.Next() {
		var reply TweetsResForHTTPGet
		if err := rows.Scan(&reply.Id, &reply.Content, &reply.LikesCount); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		replies = append(replies, reply)
	}

	// レスポンス構築
	result := struct {
		ParentTweet TweetsResForHTTPGet   `json:"parent_tweet"`
		Replies     []TweetsResForHTTPGet `json:"replies"`
	}{
		ParentTweet: parentTweet,
		Replies:     replies,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
