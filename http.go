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

func serveHTTP(config *ConfigST) {
	router := gin.Default()
	ginCfg := cors.DefaultConfig()
	ginCfg.AllowAllOrigins = true
	router.Use(cors.New(ginCfg))

	router.GET("/list", func(c *gin.Context) {
		_, all := config.List()
		sort.Strings(all)
		c.JSON(200, all)
	})
	router.GET("/status", func(c *gin.Context) {
		c.JSON(200, config)
	})
	router.GET("/ws/:suuid", func(c *gin.Context) {
		wshandler(config, c.Writer, c.Request)
	})
	router.GET("/hls/:file", func(c *gin.Context) {
		file := c.Param("file")
		c.Header("Cache-Control", "no-cache")

		c.FileFromFS(file, http.Dir("./hls"))
	})

	httpServer := &http.Server{
		Addr:         config.Server.HTTPPort,
		Handler:      router,
		ReadTimeout:  time.Duration(config.Server.HTTPTimeout) * time.Second,
		WriteTimeout: time.Duration(config.Server.HTTPTimeout) * time.Second,
	}
	err := httpServer.ListenAndServe()
	//err := router.Run(Config.Server.HTTPPort)
	if err != nil {
		log.Fatalln(err)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func wshandler(config *ConfigST, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to set websocket upgrade: %+v\n", err)
		return
	}
	defer func() {
		err = conn.Close()
		if err != nil {
			log.Print(err)
		}
		log.Println("WebSocket connection terminated.")
	}()

	suuid := r.FormValue("suuid")
	log.Println("Request", suuid)
	if config.Ext(suuid) {
		conn.SetWriteDeadline(time.Now().Add(50 * time.Second))
		cuuid, ch := config.ClientAdd(suuid)
		defer config.ClientDelete(suuid, cuuid)
		codecs := config.CodecGet(suuid)
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
					conn.SetWriteDeadline(time.Now().Add(100 * time.Second))
					err := conn.WriteMessage(websocket.BinaryMessage, buf)
					if err != nil {
						return
					}
				}
			}
		}
	}
}
