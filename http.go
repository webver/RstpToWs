package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/webver/vdk/format/mp4f"
	"golang.org/x/net/websocket"
)

func serveHTTP() {
	router := gin.Default()
	router.GET("/status", func(c *gin.Context) {
		c.JSON(200, Config)
	})
	router.GET("/ws/:suuid", func(c *gin.Context) {
		handler := websocket.Handler(ws)
		handler.ServeHTTP(c.Writer, c.Request)
	})
	err := router.Run(Config.Server.HTTPPort)
	if err != nil {
		log.Fatalln(err)
	}
}
func ws(ws *websocket.Conn) {
	defer ws.Close()
	suuid := ws.Request().FormValue("suuid")
	log.Println("Request", suuid)
	if Config.ext(suuid) {
		ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		suuid := ws.Request().FormValue("suuid")
		cuuid, ch := Config.clientAdd(suuid)
		defer func() {
			Config.clientDelete(suuid, cuuid)
			ws.Close()
			log.Println("Close ws")
		}()
		codecs := Config.codecGet(suuid)
		if codecs == nil {
			log.Println("No Codec Info")
			return
		}
		muxer := mp4f.NewMuxer(nil)
		muxer.WriteHeader(codecs)
		meta, init := muxer.GetInit(codecs)
		err := websocket.Message.Send(ws, append([]byte{9}, meta...))
		if err != nil {
			return
		}
		err = websocket.Message.Send(ws, init)
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
					err := websocket.Message.Send(ws, buf)
					if err != nil {
						return
					}
				}
			}
		}
	}
}
