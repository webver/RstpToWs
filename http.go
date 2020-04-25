package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/webver/vdk/format/mp4f"
)

func serveHTTP() {
	router := gin.Default()
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
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to set websocket upgrade: %+v\n", err)
		return
	}
	defer func() {
		cm := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "")
		err = ws.WriteMessage(websocket.CloseMessage, cm)
		if err != nil {
			log.Print(err)
		}
		err = ws.Close()
		if err != nil {
			log.Print(err)
		}
	}()
	suuid := r.FormValue("suuid")
	log.Println("Request", suuid)
	if Config.ext(suuid) {
		ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
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
		err := ws.WriteMessage(websocket.BinaryMessage, append([]byte{9}, meta...))
		if err != nil {
			return
		}
		err = ws.WriteMessage(websocket.BinaryMessage, init)
		if err != nil {
			return
		}
		var start bool
		for {
			select {
			case pck := <-ch:
				if pck.IsKeyFrame {
					start = true
				}
				if !start {
					continue
				}
				ready, buf, _ := muxer.WritePacket(pck, false)
				if ready {
					ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
					err := ws.WriteMessage(websocket.BinaryMessage, buf)
					if err != nil {
						return
					}
				}
			}
		}
	}
}
