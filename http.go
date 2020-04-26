package main

import (
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/webver/vdk/format/mp4f"
)

func serveHTTP() {
	router := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	router.Use(cors.New(config))

	router.GET("/list", func(c *gin.Context) {
		_, all := Config.list()
		sort.Strings(all)
		c.JSON(200, all)
	})
	router.GET("/status", func(c *gin.Context) {
		c.JSON(200, Config)
	})
	router.GET("/ws/:suuid", func(c *gin.Context) {
		wshandler(c.Writer, c.Request)
	})
	err := router.Run(Config.Server.HTTPPort)
	if err != nil {
		log.Fatalln(err)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wshandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to set websocket upgrade: %+v\n", err)
		return
	}
	defer func() {
		cm := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "")
		err = conn.WriteMessage(websocket.CloseMessage, cm)
		if err != nil {
			log.Print(err)
		}
		err = conn.Close()
		if err != nil {
			log.Print(err)
		}
		log.Println("WebSocket connection terminated.")
	}()

	suuid := r.FormValue("suuid")
	log.Println("Request", suuid)
	if Config.ext(suuid) {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		cuuid, ch := Config.clientAdd(suuid)
		defer Config.clientDelete(suuid, cuuid)
		codecs := Config.codecGet(suuid)
		if codecs == nil {
			log.Println("No Codec Info")
			return
		}
		muxer := mp4f.NewMuxer(nil)
		muxer.WriteHeader(codecs)
		meta, init := muxer.GetInit(codecs)
		err := conn.WriteMessage(websocket.BinaryMessage, append([]byte{9}, meta...))
		if err != nil {
			return
		}
		err = conn.WriteMessage(websocket.BinaryMessage, init)
		if err != nil {
			return
		}
		var start bool
		quitCh := make(chan bool)
		go func(q chan bool) {
			//Читаем что бы отрабаытваел onClose
			_, _, err := conn.ReadMessage()
			if err != nil {
				q <- true
				return
			}
		}(quitCh)
		for {
			select {
			case <-quitCh:
				return
			case pck := <-ch:
				if pck.IsKeyFrame {
					start = true
				}
				if !start {
					continue
				}
				ready, buf, _ := muxer.WritePacket(pck, false)
				if ready {
					conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					err := conn.WriteMessage(websocket.BinaryMessage, buf)
					if err != nil {
						return
					}
				}
			}
		}
	}
}
