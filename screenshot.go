package videoserver

import (
	"errors"
	"fmt"
	"github.com/webver/vdk/av"
	"sync"

	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log"
	"time"
)

type RecordApp struct {
	m     sync.Mutex
	store map[uuid.UUID]StreamSaver
}

type StreamSaver struct {
	ramStore  *RamStore
	mp4writer *Mp4Writer
}

func (app *Application) StartRecordApp(cfg *ConfigurationArgs) {

	app.RecordApp.store = make(map[uuid.UUID]StreamSaver)

	for i := range cfg.Streams {
		validUUID, err := uuid.Parse(cfg.Streams[i].GUID)
		if err != nil {
			log.Printf("Not valid UUID: %s\n", cfg.Streams[i].GUID)
			continue
		}

		if cfg.Streams[i].RecordStream && cfg.Streams[i].FileSize > 0 {
			streamSaver := StreamSaver{}
			streamSaver.ramStore = NewRamStore(cfg.Streams[i].FileSize)

			streamSaver.mp4writer, err = NewMp4Writer(cfg.Mp4Directory, validUUID, cfg.Streams[i].FileSize, cfg.Streams[i].FileCount)
			if err != nil {
				log.Panic("Can't crate mp4 writer", err)
				return
			}

			log.Printf("Add mp4 writer for %s: %s\n", validUUID)

			app.RecordApp.m.Lock()
			app.RecordApp.store[validUUID] = streamSaver
			app.RecordApp.m.Unlock()
		}
	}

	for streamID := range app.RecordApp.store {
		go app.startRamSaver(streamID)
		go app.startHddRecorderWorkingLoop(streamID)
	}
}

//func (app *Application) startStreamRecorder(streamID uuid.UUID) {
//
//}

func (app *Application) startHddRecorderWorkingLoop(streamID uuid.UUID) {
	app.RecordApp.m.Lock()
	m4Writer := app.RecordApp.store[streamID].mp4writer
	app.RecordApp.m.Unlock()

	for {
		cuuid, viewer, err := app.clientAdd(streamID)
		if err != nil {
			log.Printf("Can't add client for '%s' due the error: %s\n", streamID, err.Error())
			return
		}

		status, err := app.getStatus(streamID)
		if err != nil {
			log.Printf("Can't get status data for '%s' due the error: %s", streamID, err.Error())
		}

		codecData, err := app.codecGet(streamID)
		if err != nil {
			log.Printf("Can't get codec data for '%s' due the error: %s", streamID, err.Error())
		}

		if status && codecData != nil {
			//Перезапуск при реконнектах камеры
			log.Printf("start mp4writer: %s\n", streamID)
			err = m4Writer.startMp4Writer(codecData, viewer.c, viewer.status)
			if err != nil {
				log.Printf("Mp4 writer for '%s' stopped: %s", streamID, err.Error())
			} else {
				log.Printf("Mp4 writer for '%s' stopped", streamID)
			}
		} else {
			log.Printf("Status is false or codec data is nil for '%s'", streamID)
		}

		app.clientDelete(streamID, cuuid)

		if !app.exists(streamID) {
			log.Printf("Close recorder loop for '%s'", streamID)
			return
		}

		time.Sleep(1 * time.Second)
	}

}

func (app *Application) startRamSaver(streamID uuid.UUID) {
	app.RecordApp.m.Lock()
	ramStore := app.RecordApp.store[streamID].ramStore
	app.RecordApp.m.Unlock()

	if app.existsWithType(streamID, "mse") {
		cuuid, viewer, err := app.clientAdd(streamID)
		if err != nil {
			log.Printf("Can't add client for '%s' due the error: %s\n", streamID, err.Error())
			return
		}
		defer app.clientDelete(streamID, cuuid)

		for {
			select {
			case pck := <-viewer.c:
				if pck.Idx == 0 { //Пишем только видео
					err = ramStore.AppendPkt(pck)
					if err != nil {
						log.Printf("Can't append packet for stream '%s' due the error: %s\n", streamID, err.Error())
					}
				}
			case status := <-viewer.status:
				if status == false {
					err = ramStore.Clear()
					if err != nil {
						log.Printf("Can't clear ram buffer for stream '%s' due the error: %s\n", streamID, err.Error())
					}
				}
			}
		}
	}
}

func screenShotWrapper(ctx *gin.Context, app *Application) {
	start := time.Now()

	cuuid := ctx.Param("suuid")
	streamID, err := uuid.Parse(uuidRegExp.FindString(cuuid))
	if err != nil {
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	}

	timeT := time.Now().Add(time.Second * time.Duration(-5))

	timestampStr := ctx.Query("timestamp")
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	} else {

		timeT = time.Unix(timestamp, 0)
		msStr := ctx.Query("ms")
		ms, err := strconv.ParseInt(msStr, 10, 64)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		} else {
			timeT = time.Unix(timestamp, ms*1000000)
		}
	}

	ctx.Header("Cache-Control", "no-cache")

	var pktArray []av.Packet
	var codecData []av.CodecData

	measureStart := time.Now()

	app.RecordApp.m.Lock()
	streamSaver, ok := app.RecordApp.store[streamID]
	app.RecordApp.m.Unlock()
	if !ok {
		err = errors.New(fmt.Sprintf("Поток %s не найден", streamID.String()))
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	}

	if timeT.After(streamSaver.ramStore.firstPktTime) && timeT.Before(streamSaver.ramStore.lastPktTime) {
		pktArray, err = streamSaver.ramStore.FindPacket(timeT)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		}

		log.Printf("Время разбора H264 %v", time.Since(measureStart))
		log.Printf("Пакеты найдены в памяти")

		codecData, err = app.codecGet(streamID)
		if err != nil {
			log.Printf("Can't add client '%s' due the error: %s\n", streamID, err.Error())
			ctx.JSON(404, err.Error())
			return
		}
		if codecData == nil || len(codecData) == 0 || codecData[0].Type() != av.H264 {
			log.Printf("No codec information for stream %s\n", streamID)
			ctx.JSON(404, err.Error())
			return
		}
	} else {
		fileName, segmentStartTime, err := streamSaver.mp4writer.segmentStore.FindSegment(timeT)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		}

		timeInFile := timeT.Sub(segmentStartTime)

		codecData, pktArray, err = streamSaver.mp4writer.FindInFile(fileName, timeInFile)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		}

		log.Printf("Пакеты найдены на диске")
	}

	if codecData == nil || len(codecData) == 0 || codecData[0].Type() != av.H264 {
		log.Printf("No codec information for stream %s\n", streamID)
		ctx.JSON(404, err.Error())
		return
	}

	log.Printf("Время получения пакетов %v", time.Since(measureStart))

	log.Printf("Время получения изображения суммарно %v", time.Since(start))
	ctx.JSON(200, pktArray)

	//decoder, err := ffmpeg.NewVideoDecoder(codecData[0])
	//if err != nil {
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	//
	//measureStart = time.Now()
	//
	//var img *ffmpeg.VideoFrame
	////for i := range pktArray {
	////err = decoder.DecoderAppendPkt(pktArray[i].Data)
	////if err != nil {
	////	log.Print(err)
	////	ctx.JSON(404, err.Error())
	////	return
	////}
	////
	////img, err = decoder.DecoderReceiveFrame()
	//img, err = decoder.DecodeNewApi(pktArray)
	//if err != nil {
	//	err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	////if i != len(pktArray) -1 {
	////	img.Free()
	////}
	////}
	//
	//log.Printf("Время декодирования %v", time.Since(measureStart))
	//measureStart = time.Now()
	//
	//decoder.Free()
	//
	//log.Printf("Время получения изображения %v", time.Since(measureStart))
	//measureStart = time.Now()
	//
	//if img == nil {
	//	err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	//
	//buf := new(bytes.Buffer)
	//err = jpeg.Encode(buf, &img.Image, &jpeg.EncoderOptions{Quality: 90})
	//if err != nil {
	//	err = errors.New(fmt.Sprintf("Ошибка преобразования в jpeg для времени %v", timeT))
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	//
	//img.Free()
	//
	//log.Printf("Время преобразования в JPEG %v", time.Since(measureStart))
	//log.Printf("Время получения изображения суммарно %v", time.Since(start))
	//
	//ctx.Data(200, "image/jpeg", buf.Bytes())

	return
}
