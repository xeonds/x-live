// main.go
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB
var upgrader = websocket.Upgrader{}

type Stream struct {
	ID        int
	StreamKey string
	HashKey   string
	HTTPURL   string
	CreatedAt time.Time
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./streams.db")
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS streams (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		stream_key TEXT UNIQUE,
		hash_key TEXT UNIQUE,
		http_url TEXT,
		created_at DATETIME
	)`)
	if err != nil {
		log.Fatal(err)
	}
}

func generateHash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])[:8]
}

func handleCommands() {
	if len(os.Args) < 2 {
		return
	}

	cmd := os.Args[1]
	switch cmd {
	case "add":
		key := os.Args[2]
		hash := generateHash(key)
		httpUrl := fmt.Sprintf("http://localhost/live/%s.flv", hash)
		_, err := db.Exec("INSERT INTO streams (stream_key, hash_key, http_url, created_at) VALUES (?, ?, ?, ?)",
			key, hash, httpUrl, time.Now())
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Added: %s\nRTMP URL: %s\nHTTP URL: %s\n", key, "rtmp://localhost:1935/live", httpUrl)
	case "list":
		rows, err := db.Query("SELECT id, stream_key, http_url FROM streams")
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, rtmp, http string
			rows.Scan(&id, &rtmp, &http)
			fmt.Printf("%s RTMP: %s\nHTTP: %s\n\n", id, rtmp, http)
		}
	case "delete":
		id := os.Args[2]
		_, err := db.Exec("DELETE FROM streams WHERE id = ?", id)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Deleted stream", id)
	}
	os.Exit(0)
}

func main() {
	initDB()
	handleCommands()

	r := gin.Default()

	// WebSocket hub
	clients := make(map[*websocket.Conn]string)
	wsChan := make(chan string)

	go func() {
		for msg := range wsChan {
			for client := range clients {
				client.WriteMessage(websocket.TextMessage, []byte(msg))
			}
		}
	}()

	// API Routes
	r.GET("/ws", func(c *gin.Context) {
		conn, _ := upgrader.Upgrade(c.Writer, c.Request, nil)
		ip, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
		userID := generateHash(ip)
		clients[conn] = userID

		defer func() {
			delete(clients, conn)
			conn.Close()
		}()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			wsChan <- fmt.Sprintf("[%s]: %s", userID[:4], msg)
		}
	})
	// 实现nginx的on_publish接口
	r.POST("/on_publish", func(c *gin.Context) {
		streamKey := c.PostForm("name")
		var hashKey string

		err := db.QueryRow("SELECT hash_key FROM streams WHERE stream_key = ?", streamKey).Scan(&hashKey)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid stream key"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Stream key is valid"})
	})

	r.GET("/live/:hash", func(c *gin.Context) {
		c.File("./index.html")
	})

	r.StaticFS("/static", http.Dir("./static"))

	fmt.Println("Server running on :8080")
	r.Run("0.0.0.0:8080")
}
