package videoserver

import (
	"github.com/google/uuid"
	"github.com/webver/vdk/format/rtspv2"
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
		app.StartStream(k)
	}
}

func (app *Application) CloseStreams() {
	for _, k := range app.Streams.getKeys() {
		app.CloseStream(k)
	}
}

func (app *Application) StreamWorkerLoop(streamID uuid.UUID) {
	defer app.runUnlock(streamID)
	app.Streams.Lock()
	onDemand := app.Streams.Streams[streamID].OnDemand
	app.Streams.Unlock()

	for {
		log.Println(streamID.String(), "Stream Try Connect")
		err := app.StreamWorker(streamID)
		if err != nil {
			log.Println(err)
		} else {
			//Закрытие потока ошибки
			app.Streams.Lock()
			delete(app.Streams.Streams, streamID)
			app.Streams.Unlock()
			log.Println(streamID.String(), "Close stream worker loop")
			return
		}

		if onDemand && !app.hasViewer(streamID) {
			log.Println(streamID, ErrorStreamExitNoViewer)
			return
		}

		time.Sleep(1 * time.Second)
	}
}

func (app *Application) StreamWorker(streamID uuid.UUID) error {
	app.Streams.Lock()
	url := app.Streams.Streams[streamID].URL
	onDemand := app.Streams.Streams[streamID].OnDemand
	closeGracefully := app.Streams.Streams[streamID].closeGracefully
	app.Streams.Unlock()

	keyTest := time.NewTimer(20 * time.Second)
	clientTest := time.NewTimer(20 * time.Second)
	RTSPClient, err := rtspv2.Dial(rtspv2.RTSPClientOptions{URL: url, DisableAudio: true, DialTimeout: 3 * time.Second, ReadWriteTimeout: 3 * time.Second, Debug: true})
	if err != nil {
		return err
	}
	defer RTSPClient.Close()
	if RTSPClient.CodecData != nil {
		app.codecAdd(streamID, RTSPClient.CodecData)
		err = app.updateStatus(streamID, true)
		if err != nil {
			log.Printf("Can't update status 'true' for %s: %s\n", streamID, err.Error())
		}
	}
	var AudioOnly bool
	if len(RTSPClient.CodecData) == 1 && RTSPClient.CodecData[0].Type().IsAudio() {
		AudioOnly = true
	}
	for {
		select {
		case <-clientTest.C:
			if onDemand && !app.hasViewer(streamID) {
				return ErrorStreamExitNoViewer
			}
		case <-keyTest.C:
			log.Printf("No video on stream %s:\n", streamID)
			return ErrorStreamExitNoVideoOnStream
		case signals := <-RTSPClient.Signals:
			switch signals {
			case rtspv2.SignalCodecUpdate:
				app.codecAdd(streamID, RTSPClient.CodecData)
				err = app.updateStatus(streamID, true)
				if err != nil {
					log.Printf("Can't update status 'true' for %s: %s\n", streamID, err.Error())
				}
			case rtspv2.SignalStreamRTPStop:
				err = app.updateStatus(streamID, false)
				if err != nil {
					log.Printf("Can't update status 'true' for %s: %s\n", streamID, err.Error())
				}
				return ErrorStreamExitRtspDisconnect
			}
		case packetAV := <-RTSPClient.OutgoingPacketQueue:
			if AudioOnly || packetAV.IsKeyFrame {
				keyTest.Reset(20 * time.Second)
			}
			err = app.castMSE(streamID, *packetAV)
			if err != nil {
				log.Printf("Can't cast for %s: %s\n", streamID, err.Error())
			}
		case <-closeGracefully:
			err = app.updateStatus(streamID, false)
			return nil
		}
	}
}

func (app *Application) StartStream(streamID uuid.UUID) {
	go app.StreamWorkerLoop(streamID)
	if app.existsWithType(streamID, "hls") {
		go app.startHlsWorkerLoop(streamID)
	}
}

func (app *Application) CloseStream(streamID uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	app.Streams.Streams[streamID].closeGracefully <- true
}
