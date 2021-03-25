package videoserver

import (
	"bytes"
	"fmt"
	"github.com/LdDl/vdk/av"
	"github.com/LdDl/vdk/cgo/ffmpeg"
	"github.com/pkg/errors"
	"image"
	"image/color"
	//"image/jpeg"
	"github.com/pixiv/go-libjpeg/jpeg"
	"strconv"

	//"github.com/LdDl/vdk/cgo/ffmpeg"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"log"
	"time"
)

type RecordApp struct {
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

		if cfg.Streams[i].RecordStream && cfg.Streams[i].RamSize > 0 {
			streamSaver := StreamSaver{}
			streamSaver.ramStore = NewRamStore(cfg.Streams[i].RamSize)

			streamSaver.mp4writer, err = NewMp4Writer(cfg.Mp4Directory, validUUID, cfg.Mp4FileSize, cfg.Mp4FileCount)
			if err != nil {
				log.Panic("Can't crate mp4 writer", err)
				return
			}

			app.RecordApp.store[validUUID] = streamSaver

		}
	}

	for streamID := range app.RecordApp.store {
		go app.StartRamSaver(streamID)
		go app.StartHddSaver(streamID)
	}
}

func (app *Application) StartHddSaver(streamID uuid.UUID) {

	if app.existsWithType(streamID, "mse") {
		cuuid, viewer, err := app.clientAdd(streamID)
		if err != nil {
			log.Printf("Can't add client for '%s' due the error: %s\n", streamID, err.Error())
			return
		}
		defer app.clientDelete(streamID, cuuid)

		quitCh := make(chan bool, 1)

		for {
			select {
			case status := <-viewer.status: //Перезапускает запись при реконнектах камеры
				if status == true {
					codecData, err := app.codecGet(streamID)
					if err != nil {
						log.Printf("Can't get codec data for '%s' due the error: %s\n", streamID, err.Error())
					}

					if codecData != nil {
						go app.RecordApp.store[streamID].mp4writer.startMp4Writer(codecData, viewer.c, quitCh)
					}
				} else {
					if len(quitCh) < cap(quitCh) {
						quitCh <- false
					}
				}
			}
		}
	}
}

func (app *Application) StartRamSaver(streamID uuid.UUID) {

	quitCh := make(chan bool, 1)

	if app.existsWithType(streamID, "mse") {
		cuuid, viewer, err := app.clientAdd(streamID)
		if err != nil {
			log.Printf("Can't add client for '%s' due the error: %s\n", streamID, err.Error())
			return
		}
		defer app.clientDelete(streamID, cuuid)

		for {
			select {
			case <-quitCh:
				app.RecordApp.store[streamID].ramStore.Clear()
				return
			case pck := <-viewer.c:
				if pck.Idx == 0 { //Пишем только видео
					app.RecordApp.store[streamID].ramStore.AppendPkt(pck)
				}
			}
		}
	}
}

func convertYcbCrImageToRgbaImage(original image.YCbCr) *image.RGBA {
	bounds := original.Bounds()
	converted := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))

	i := 0
	for row := 0; row < bounds.Max.Y; row++ {
		for col := 0; col < bounds.Max.X; col++ {
			clr := original.YCbCrAt(col, row)
			r, g, b := color.YCbCrToRGB(clr.Y, clr.Cb, clr.Cr)
			converted.Pix[i+0] = uint8(r)
			converted.Pix[i+1] = uint8(g)
			converted.Pix[i+2] = uint8(b)
			converted.Pix[i+3] = uint8(0xFF)
			i += 4
		}
	}
	return converted
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
		nanoStr := ctx.Query("nano")
		nano, err := strconv.ParseInt(nanoStr, 10, 64)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		} else {
			timeT = time.Unix(timestamp, nano)
		}
	}

	ctx.Header("Cache-Control", "no-cache")

	var pktArray []av.Packet
	var codecData []av.CodecData

	measureStart := time.Now()

	if timeT.After(app.RecordApp.store[streamID].ramStore.firstPktTime) && timeT.Before(app.RecordApp.store[streamID].ramStore.lastPktTime) {
		pktArray, err = app.RecordApp.store[streamID].ramStore.FindPacket(timeT)
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
		fileName, segmentStartTime, err := app.RecordApp.store[streamID].mp4writer.segmentStore.FindSegment(timeT)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		}

		timeInFile := timeT.Sub(segmentStartTime)

		codecData, pktArray, err = app.RecordApp.store[streamID].mp4writer.FindInFile(fileName, timeInFile)
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

	decoder, err := ffmpeg.NewVideoDecoder(codecData[0])
	if err != nil {
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	}

	err = decoder.Setup()
	if err != nil {
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	}

	measureStart = time.Now()

	var img *ffmpeg.VideoFrame
	for i := range pktArray {
		err = decoder.DecoderAppendPkt(pktArray[i].Data)
		if err != nil {
			log.Print(err)
			ctx.JSON(404, err.Error())
			return
		}
	}

	log.Printf("Время декодирования %v", time.Since(measureStart))
	measureStart = time.Now()

	img, err = decoder.DecoderReceiveFrame()
	if err != nil {
		err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	}

	log.Printf("Время получения изображения %v", time.Since(measureStart))
	measureStart = time.Now()

	if img == nil {
		err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	} else {
		defer img.Free()
	}

	buf := new(bytes.Buffer)
	jpeg.Encode(buf, &img.Image, &jpeg.EncoderOptions{Quality: 90})

	log.Printf("Время преобразования в JPEG %v", time.Since(measureStart))
	log.Printf("Время получения изображения суммарно %v", time.Since(start))

	ctx.Data(200, "image/jpeg", buf.Bytes())

	//ctx.JSON(200, pktArray)
	return
}
