package videoserver

import (
	"github.com/deepch/vdk/format/rtspv2"
	"github.com/google/uuid"
	"log"
	"time"
)

func typeExists(typeName string, typesNames []string) bool {
	for i := range typesNames {
		if typesNames[i] == typeName {
			return true
		}
	}
	return false
}

// StartStreams Start video streams
func (app *Application) StartStreams() {

	for _, k := range app.Streams.getKeys() {
		//app.Streams.Lock()
		//supportedTypes := app.Streams.Streams[k].SupportedStreamTypes
		//app.Streams.Unlock()

		//hlsEnabled := typeExists("hls", supportedTypes)

		go app.StreamWorkerLoop(k)
	}
}

func (app *Application) StreamWorkerLoop(k uuid.UUID) {
	defer app.runUnlock(k)
	for {
		app.Streams.Lock()
		onDemand := app.Streams.Streams[k].OnDemand
		app.Streams.Unlock()

		log.Println(k.String(), "Stream Try Connect")
		err := app.StreamWorker(k)
		if err != nil {
			log.Println(err)
		}

		if onDemand && !app.hasViewer(k) {
			log.Println(k, ErrorStreamExitNoViewer)
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func (app *Application) StreamWorker(k uuid.UUID) error {
	app.Streams.Lock()
	url := app.Streams.Streams[k].URL
	onDemand := app.Streams.Streams[k].OnDemand
	app.Streams.Unlock()

	keyTest := time.NewTimer(20 * time.Second)
	clientTest := time.NewTimer(20 * time.Second)
	RTSPClient, err := rtspv2.Dial(rtspv2.RTSPClientOptions{URL: url, DisableAudio: true, DialTimeout: 3 * time.Second, ReadWriteTimeout: 3 * time.Second, Debug: false})
	if err != nil {
		return err
	}
	defer RTSPClient.Close()
	if RTSPClient.CodecData != nil {
		app.codecAdd(k, RTSPClient.CodecData)
		err = app.updateStatus(k, true)
		if err != nil {
			log.Printf("Can't update status 'true' for %s: %s\n", k, err.Error())
		}
	}
	var AudioOnly bool
	if len(RTSPClient.CodecData) == 1 && RTSPClient.CodecData[0].Type().IsAudio() {
		AudioOnly = true
	}
	for {
		select {
		case <-clientTest.C:
			if onDemand && !app.hasViewer(k) {
				return ErrorStreamExitNoViewer
			}
		case <-keyTest.C:
			return ErrorStreamExitNoVideoOnStream
		case signals := <-RTSPClient.Signals:
			switch signals {
			case rtspv2.SignalCodecUpdate:
				app.codecAdd(k, RTSPClient.CodecData)
				err = app.updateStatus(k, true)
				if err != nil {
					log.Printf("Can't update status 'true' for %s: %s\n", k, err.Error())
				}
			case rtspv2.SignalStreamRTPStop:
				err = app.updateStatus(k, false)
				if err != nil {
					log.Printf("Can't update status 'true' for %s: %s\n", k, err.Error())
				}
				return ErrorStreamExitRtspDisconnect
			}
		case packetAV := <-RTSPClient.OutgoingPacketQueue:
			if AudioOnly || packetAV.IsKeyFrame {
				keyTest.Reset(20 * time.Second)
			}
			app.castMSE(k, *packetAV)
		}
	}
}

////
//// StartStream Start video stream
//func (app *Application) StartStream(k uuid.UUID) error  {
//	app.Streams.Lock()
//	url := app.Streams.Streams[k].URL
//	supportedTypes := app.Streams.Streams[k].SupportedStreamTypes
//	app.Streams.Unlock()
//
//	hlsEnabled := typeExists("hls", supportedTypes)
//
//	for {
//		log.Println(k, "Stream Try Connect")
//		err := RTSPWorker(k, url, OnDemand)
//		if err != nil {
//			log.Println(err)
//		}
//		if OnDemand && !Config.HasViewer(name) {
//			log.Println(name, ErrorStreamExitNoViewer)
//			return
//		}
//		time.Sleep(1 * time.Second)
//	}
//}

//// StartStreams Start video streams
//func (app *Application) StartStreams() {
//
//	for _, k := range app.Streams.getKeys() {
//		app.Streams.Lock()
//		url := app.Streams.Streams[k].URL
//		supportedTypes := app.Streams.Streams[k].SupportedStreamTypes
//		app.Streams.Unlock()
//
//		hlsEnabled := typeExists("hls", supportedTypes)
//
//		go func(name uuid.UUID, hlsEnabled bool, url string) {
//			for {
//				log.Printf("Stream must be establishment for '%s' by connecting to %s", name, url)
//				rtsp.DebugRtsp = false
//				session, err := rtsp.Dial(url)
//				if err != nil {
//					hlserror.SetError(name, 502, fmt.Errorf("rtsp.Dial error for %s (%s): %s", name, url, err.Error()))
//					log.Printf("rtsp.Dial error for %s (%s): %s\n", name, url, err.Error())
//					time.Sleep(60 * time.Second)
//					continue
//				}
//				session.RtpKeepAliveTimeout = time.Duration(10 * time.Second)
//				codec, err := session.Streams()
//				if err != nil {
//					hlserror.SetError(name, 520, fmt.Errorf("Can't get sessions for %s (%s): %s", name, url, err.Error()))
//					log.Printf("Can't get sessions for %s (%s): %s\n", name, url, err.Error())
//					time.Sleep(60 * time.Second)
//					continue
//				}
//				app.codecAdd(name, codec)
//				err = app.updateStatus(name, true)
//				if err != nil {
//					log.Printf("Can't update status 'true' for %s (%s): %s\n", name, url, err.Error())
//					time.Sleep(60 * time.Second)
//					continue
//				}
//
//				if hlsEnabled {
//					stopHlsCast := make(chan bool, 1)
//					app.startHlsCast(name, stopHlsCast)
//					for {
//						pkt, err := session.ReadPacket()
//						if err != nil {
//							hlserror.SetError(name, 500, fmt.Errorf("Can't read session's packet %s (%s): %s", name, url, err.Error()))
//							log.Printf("Can't read session's packet %s (%s): %s\n", name, url, err.Error())
//							stopHlsCast <- true
//							break
//						}
//						err = app.cast(name, pkt)
//						if err != nil {
//							hlserror.SetError(name, 500, fmt.Errorf("Can't cast packet %s (%s): %s", name, url, err.Error()))
//							log.Printf("Can't cast packet %s (%s): %s\n", name, url, err.Error())
//							stopHlsCast <- true
//							break
//						}
//					}
//				} else {
//					for {
//						pkt, err := session.ReadPacket()
//						if err != nil {
//							log.Printf("Can't read session's packet %s (%s): %s\n", name, url, err.Error())
//							break
//						}
//
//						err = app.castMSE(name, pkt)
//						if err != nil {
//							log.Printf("Can't cast packet %s (%s): %s\n", name, url, err.Error())
//							break
//						}
//					}
//				}
//
//				session.Close()
//				err = app.updateStatus(name, false)
//				if err != nil {
//					log.Printf("Can't update status 'false' for %s (%s): %s\n", name, url, err.Error())
//					time.Sleep(60 * time.Second)
//					continue
//				}
//				log.Printf("Stream must be re-establishment for '%s' by connecting to %s in next 5 seconds\n", name, url)
//				time.Sleep(5 * time.Second)
//			}
//		}(k, hlsEnabled, url)
//	}
//}
//
////
////// StartStream Start video stream
////func (app *Application) StartStream(k uuid.UUID) {
////	app.Streams.Lock()
////	url := app.Streams.Streams[k].URL
////	supportedTypes := app.Streams.Streams[k].SupportedStreamTypes
////	app.Streams.Unlock()
////
////	hlsEnabled := typeExists("hls", supportedTypes)
////
////	go func(name uuid.UUID, hlsEnabled bool, url string) {
////		for {
////			log.Printf("Stream must be establishment for '%s' by connecting to %s", name, url)
////			rtsp.DebugRtsp = false
////			session, err := rtsp.Dial(url)
////			if err != nil {
////				hlserror.SetError(name, 502, fmt.Errorf("rtsp.Dial error for %s (%s): %s", name, url, err.Error()))
////				log.Printf("rtsp.Dial error for %s (%s): %s\n", name, url, err.Error())
////				time.Sleep(60 * time.Second)
////				continue
////			}
////			session.RtpKeepAliveTimeout = time.Duration(10 * time.Second)
////			codec, err := session.Streams()
////			if err != nil {
////				hlserror.SetError(name, 520, fmt.Errorf("Can't get sessions for %s (%s): %s", name, url, err.Error()))
////				log.Printf("Can't get sessions for %s (%s): %s\n", name, url, err.Error())
////				time.Sleep(60 * time.Second)
////				continue
////			}
////			app.codecAdd(name, codec)
////			err = app.updateStatus(name, true)
////			if err != nil {
////				log.Printf("Can't update status 'true' for %s (%s): %s\n", name, url, err.Error())
////				time.Sleep(60 * time.Second)
////				continue
////			}
////
////			if hlsEnabled {
////				stopHlsCast := make(chan bool, 1)
////				app.startHlsCast(name, stopHlsCast)
////				for {
////					pkt, err := session.ReadPacket()
////					if err != nil {
////						hlserror.SetError(name, 500, fmt.Errorf("Can't read session's packet %s (%s): %s", name, url, err.Error()))
////						log.Printf("Can't read session's packet %s (%s): %s\n", name, url, err.Error())
////						stopHlsCast <- true
////						break
////					}
////					err = app.cast(name, pkt)
////					if err != nil {
////						hlserror.SetError(name, 500, fmt.Errorf("Can't cast packet %s (%s): %s", name, url, err.Error()))
////						log.Printf("Can't cast packet %s (%s): %s\n", name, url, err.Error())
////						stopHlsCast <- true
////						break
////					}
////				}
////			} else {
////				for {
////					pkt, err := session.ReadPacket()
////					if err != nil {
////						log.Printf("Can't read session's packet %s (%s): %s\n", name, url, err.Error())
////						break
////					}
////					err = app.castMSE(name, pkt)
////					if err != nil {
////						log.Printf("Can't cast packet %s (%s): %s\n", name, url, err.Error())
////						break
////					}
////				}
////			}
////
////			session.Close()
////			err = app.updateStatus(name, false)
////			if err != nil {
////				log.Printf("Can't update status 'false' for %s (%s): %s\n", name, url, err.Error())
////				time.Sleep(60 * time.Second)
////				continue
////			}
////			log.Printf("Stream must be re-establishment for '%s' by connecting to %s in next 5 seconds\n", name, url)
////			time.Sleep(5 * time.Second)
////		}
////	}(k, hlsEnabled, url)
////}
