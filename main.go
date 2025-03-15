// main.go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"x-live/protocol/hls"
	"x-live/protocol/rtmp"

	"embed"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed template/*
var templateFS embed.FS

var db *gorm.DB
var upgrader = websocket.Upgrader{}

type Stream struct {
	ID        int    `gorm:"primaryKey"`
	StreamKey string `gorm:"unique"`
	RoomId    string `gorm:"unique"`
	CreatedAt time.Time
}

func initDB() {
	var err error
	db, err = gorm.Open(sqlite.Open("streams.db"), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	db.AutoMigrate(&Stream{})
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
		roomId := os.Args[2]
		hash := generateHash(roomId)
		httpUrl := fmt.Sprintf("http://localhost:8266/live/%s", roomId)
		stream := Stream{StreamKey: hash, RoomId: roomId, CreatedAt: time.Now()}
		if err := db.Create(&stream).Error; err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Added: %s\nRTMP URL: %s\nHTTP URL: %s\n", roomId, "rtmp://localhost:1935/live", httpUrl)
	case "list":
		var streams []Stream
		if err := db.Find(&streams).Error; err != nil {
			log.Fatal(err)
		}
		for _, stream := range streams {
			fmt.Printf("%d RTMP-key: %s\nHTTP-url: /live/%s\n\n", stream.ID, stream.StreamKey, stream.RoomId)
		}
		if len(streams) == 0 {
			fmt.Println("No streams found")
		}
	case "delete":
		id := os.Args[2]
		if err := db.Delete(&Stream{}, id).Error; err != nil {
			log.Fatal(err)
		}
		fmt.Println("Deleted stream", id)
	}
	os.Exit(0)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	initDB()
	handleCommands()

	clients := make(map[*websocket.Conn]string)
	wsChan := make(chan string)

	var streams []Stream
	if err := db.Find(&streams).Error; err != nil {
		log.Fatal(err)
	}

	for range streams {
		rtmpStream := rtmp.NewRtmpStream()
		hlsServer := startHls()
		go startRtmpServer(rtmpStream, hlsServer)
	}
	startHttpServer(clients, wsChan)
}

func startHttpServer(clients map[*websocket.Conn]string, wsChan chan string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("Recovered in f", r)
			}
		}()
		for msg := range wsChan {
			for client := range clients {
				client.WriteMessage(websocket.TextMessage, []byte(msg))
			}
		}
	}()

	r := gin.Default()
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
		var stream Stream

		if err := db.Where("stream_key = ?", streamKey).First(&stream).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "message": "Invalid stream key"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "success", "message": "Stream key is valid"})
	})

	r.SetHTMLTemplate(template.Must(template.New("").ParseFS(templateFS, "template/*")))
	r.GET("/", func(c *gin.Context) {
		streams := make([]Stream, 0)
		db.Find(&streams)
		c.HTML(http.StatusOK, "index.html", gin.H{"Videos": streams})
	})
	r.GET("/watch/:room", func(c *gin.Context) {
		// room := c.Param("room")
		// rtmpUrl := fmt.Sprintf("/live/%s.flv", room)
		c.HTML(http.StatusOK, "watch.html", gin.H{})
	})

	r.Run(":8266")
}

func startRtmpServer(stream *rtmp.RtmpStream, hlsServer *hls.Server) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("RTMP server panic: ", r)
		}
	}()

	var rtmpListen net.Listener
	var err error
	rtmpListen, err = net.Listen("tcp", ":1935")
	if err != nil {
		log.Fatal(err)
	}

	var rtmpServer *rtmp.Server
	rtmpServer = rtmp.NewRtmpServer(stream, hlsServer)
	log.Println("RTMP Listen On :1935")
	rtmpServer.Serve(rtmpListen)
}

func startHls() *hls.Server {
	hlsListen, err := net.Listen("tcp", ":8267")
	if err != nil {
		log.Fatal(err)
	}

	hlsServer := hls.NewServer()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Println("HLS server panic: ", r)
			}
		}()
		log.Println("HLS listen On :8267")
		hlsServer.Serve(hlsListen)
	}()
	return hlsServer
}
