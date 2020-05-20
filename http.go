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
	router.StaticFS("/hls", http.Dir("./hls"))
	//router.POST("/recive", webRtcReceiver)
	//router.GET("/codec/:uuid", func(c *gin.Context) {
	//	c.Header("Access-Control-Allow-Origin", "*")
	//	if Config.ext(c.Param("uuid")) {
	//		codecs := Config.codecGet(c.Param("uuid"))
	//		if codecs == nil {
	//			return
	//		}
	//		b, err := json.Marshal(codecs)
	//		if err == nil {
	//			_, err = c.Writer.Write(b)
	//			if err != nil {
	//				log.Println("Write Codec Info error", err)
	//				return
	//			}
	//		}
	//	}
	//})
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
		conn.SetWriteDeadline(time.Now().Add(50 * time.Second))
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

//
//type rtcRequest struct {
//	data struct {
//		suuid string `json:"suuid" binding:"required"`
//		data  string `json:"data" binding:"required"`
//	} `json:"data"`
//}
//
//func webRtcReceiver(c *gin.Context) {
//	c.Header("Access-Control-Allow-Origin", "*")
//	var json map[string]string
//	c.BindJSON(&json)
//	suuid := ""
//	data := ""
//	for k, v := range json {
//		switch k {
//		case "suuid":
//			suuid = v
//		case "data":
//			data = v
//		}
//	}
//
//	log.Println("Request", suuid)
//	if Config.ext(suuid) {
//		/*
//
//			Get Codecs INFO
//
//		*/
//		codecs := Config.codecGet(suuid)
//		if codecs == nil {
//			log.Println("Codec error")
//			return
//		}
//		sps := codecs[0].(h264parser.CodecData).SPS()
//		pps := codecs[0].(h264parser.CodecData).PPS()
//		/*
//
//			Recive Remote SDP as Base64
//
//		*/
//		sd, err := base64.StdEncoding.DecodeString(data)
//		if err != nil {
//			log.Println("DecodeString error", err)
//			return
//		}
//		/*
//
//			Create Media MediaEngine
//
//		*/
//
//		mediaEngine := webrtc.MediaEngine{}
//		offer := webrtc.SessionDescription{
//			Type: webrtc.SDPTypeOffer,
//			SDP:  string(sd),
//		}
//		err = mediaEngine.PopulateFromSDP(offer)
//		if err != nil {
//			log.Println("PopulateFromSDP error", err)
//			return
//		}
//
//		var payloadType uint8
//		for _, videoCodec := range mediaEngine.GetCodecsByKind(webrtc.RTPCodecTypeVideo) {
//			if videoCodec.Name == "H264" && strings.Contains(videoCodec.SDPFmtpLine, "packetization-mode=1") {
//				payloadType = videoCodec.PayloadType
//				break
//			}
//		}
//		if payloadType == 0 {
//			log.Println("Remote peer does not support H264")
//			return
//		}
//		if payloadType != 126 {
//			log.Println("Video might not work with codec", payloadType)
//		}
//		log.Println("Work payloadType", payloadType)
//		api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
//
//		peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
//			ICEServers: []webrtc.ICEServer{
//				{
//					URLs:           []string{"turn:numb.viagenie.ca"},
//					Username:       "it@mgtniip.ru",
//					Credential:     "1qaz@WSX",
//					CredentialType: webrtc.ICECredentialTypePassword,
//				},
//			},
//		})
//		if err != nil {
//			log.Println("NewPeerConnection error", err)
//			return
//		}
//		/*
//
//			ADD KeepAlive Timer
//
//		*/
//		timer1 := time.NewTimer(time.Second * 20)
//		peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
//			// Register text message handling
//			d.OnMessage(func(msg webrtc.DataChannelMessage) {
//				//fmt.Printf("Message from DataChannel '%s': '%s'\n", d.Label(), string(msg.Data))
//				timer1.Reset(20 * time.Second)
//			})
//		})
//		/*
//
//			ADD Video Track
//
//		*/
//		videoTrack, err := peerConnection.NewTrack(payloadType, rand.Uint32(), "video", suuid+"_pion")
//		if err != nil {
//			log.Fatalln("NewTrack", err)
//		}
//		_, err = peerConnection.AddTransceiverFromTrack(videoTrack,
//			webrtc.RtpTransceiverInit{
//				Direction: webrtc.RTPTransceiverDirectionSendonly,
//			},
//		)
//		if err != nil {
//			log.Println("AddTransceiverFromTrack error", err)
//			return
//		}
//		_, err = peerConnection.AddTrack(videoTrack)
//		if err != nil {
//			log.Println("AddTrack error", err)
//			return
//		}
//		/*
//
//			ADD Audio Track
//
//		*/
//		var audioTrack *webrtc.Track
//		if len(codecs) > 1 && (codecs[1].Type() == av.PCM_ALAW || codecs[1].Type() == av.PCM_MULAW) {
//			switch codecs[1].Type() {
//			case av.PCM_ALAW:
//				audioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypePCMA, rand.Uint32(), "audio", suuid+"audio")
//			case av.PCM_MULAW:
//				audioTrack, err = peerConnection.NewTrack(webrtc.DefaultPayloadTypePCMU, rand.Uint32(), "audio", suuid+"audio")
//			}
//			if err != nil {
//				log.Println(err)
//				return
//			}
//			_, err = peerConnection.AddTransceiverFromTrack(audioTrack,
//				webrtc.RtpTransceiverInit{
//					Direction: webrtc.RTPTransceiverDirectionSendonly,
//				},
//			)
//			if err != nil {
//				log.Println("AddTransceiverFromTrack error", err)
//				return
//			}
//			_, err = peerConnection.AddTrack(audioTrack)
//			if err != nil {
//				log.Println(err)
//				return
//			}
//		}
//		if err := peerConnection.SetRemoteDescription(offer); err != nil {
//			log.Println("SetRemoteDescription error", err, offer.SDP)
//			return
//		}
//		log.Println("Offer sdp", offer.SDP)
//		answer, err := peerConnection.CreateAnswer(nil)
//		if err != nil {
//			log.Println("CreateAnswer error", err)
//			return
//		}
//
//		if err = peerConnection.SetLocalDescription(answer); err != nil {
//			log.Println("SetLocalDescription error", err)
//			return
//		}
//		_, err = c.Writer.Write([]byte(base64.StdEncoding.EncodeToString([]byte(answer.SDP))))
//		if err != nil {
//			log.Println("Writer SDP error", err)
//			return
//		}
//		control := make(chan bool, 10)
//		peerConnection.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
//			log.Println("OnICEGatheringStateChange ", state)
//		})
//		peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
//			log.Println("OnICECandidate ", candidate)
//		})
//		peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
//			log.Printf("Connection State has changed %s \n", connectionState.String())
//			if connectionState != webrtc.ICEConnectionStateConnected {
//				log.Println("Client Close Exit")
//				err := peerConnection.Close()
//				if err != nil {
//					log.Println("peerConnection Close error", err)
//				}
//				control <- true
//				return
//			}
//			if connectionState == webrtc.ICEConnectionStateConnected {
//				go func() {
//					cuuid, ch := Config.clientAdd(suuid)
//					log.Println("start stream", suuid, "client", cuuid)
//					defer func() {
//						log.Println("stop stream", suuid, "client", cuuid)
//						defer Config.clientDelete(suuid, cuuid)
//					}()
//					var Vpre time.Duration
//					var start bool
//					timer1.Reset(50 * time.Second)
//					for {
//						select {
//						case <-timer1.C:
//							log.Println("Client Close Keep-Alive Timer")
//							peerConnection.Close()
//						case <-control:
//							return
//						case pck := <-ch:
//							//timer1.Reset(2 * time.Second)
//							if pck.IsKeyFrame {
//								start = true
//							}
//							if !start {
//								continue
//							}
//							if pck.IsKeyFrame {
//								pck.Data = append([]byte("\000\000\001"+string(sps)+"\000\000\001"+string(pps)+"\000\000\001"), pck.Data[4:]...)
//
//							} else {
//								pck.Data = pck.Data[4:]
//							}
//							var Vts time.Duration
//							if pck.Idx == 0 && videoTrack != nil {
//								if Vpre != 0 {
//									Vts = pck.Time - Vpre
//								}
//								samples := uint32(90000 / 1000 * Vts.Milliseconds())
//								err := videoTrack.WriteSample(media.Sample{Data: pck.Data, Samples: samples})
//								if err != nil {
//									return
//								}
//								Vpre = pck.Time
//							} else if pck.Idx == 1 && audioTrack != nil {
//								err := audioTrack.WriteSample(media.Sample{Data: pck.Data, Samples: uint32(len(pck.Data))})
//								if err != nil {
//									return
//								}
//							}
//						}
//					}
//
//				}()
//			}
//		})
//		return
//	}
//}
