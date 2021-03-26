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
		go app.StreamWorkerLoop(k)
	}
}

func (app *Application) CloseStreams() {
	for _, k := range app.Streams.getKeys() {
		app.CloseStream(k)
	}
}

func (app *Application) StreamWorkerLoop(k uuid.UUID) {
	defer app.runUnlock(k)
	app.Streams.Lock()
	onDemand := app.Streams.Streams[k].OnDemand
	app.Streams.Unlock()

	for {
		log.Println(k.String(), "Stream Try Connect")
		err := app.StreamWorker(k)
		if err != nil {
			log.Println(err)
		} else {
			//Выход без ошибки
			log.Println(k.String(), "Close stream loop")
			return
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
	closeGracefully := app.Streams.Streams[k].CloseGracefully
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
		case <-closeGracefully:
			err = app.updateStatus(k, false)
			return nil
		}
	}
}

func (app *Application) CloseStream(streamID uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	app.Streams.Streams[streamID].CloseGracefully <- true
}
