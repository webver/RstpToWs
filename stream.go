package main

import (
	"log"
	"time"

	"github.com/webver/vdk/format/rtsp"
)

func serveStreams(config *ConfigST) {
	for k, v := range config.Streams {
		go func(name, url string) {
			stopHlsCast := make(chan bool, 1)
			for {
				log.Println(name, "connect", url)
				rtsp.DebugRtsp = true
				rtsp.DebugRtp = false
				session, err := rtsp.Dial(url)
				if err != nil {
					log.Println(name, err)
					time.Sleep(5 * time.Second)
					continue
				}
				session.RtpKeepAliveTimeout = time.Duration(10 * time.Second)
				if err != nil {
					log.Println(name, err)
					time.Sleep(5 * time.Second)
					continue
				}
				codec, err := session.Streams()
				if err != nil {
					log.Println(name, err)
					time.Sleep(5 * time.Second)
					continue
				}
				config.CodecAdd(name, codec)
				config.UpdateStatus(name, true)
				config.StartHlsCast(config, name, stopHlsCast)
				for {
					pkt, err := session.ReadPacket()
					if err != nil {
						log.Println(name, err)
						break
					}
					config.Cast(name, pkt)
				}
				session.Close()
				stopHlsCast <- true
				config.UpdateStatus(name, false)
				log.Println(name, "reconnect wait 5s")
				time.Sleep(5 * time.Second)
			}
		}(k, v.URL)
	}
}
