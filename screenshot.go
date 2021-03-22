package videoserver

import (
	"github.com/LdDl/vdk/av"
	"image"
	"image/color"
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
	ramStore *RamStore
}

func (app *Application) StartRecordApp(cfg *ConfigurationArgs) {

	app.RecordApp.store = make(map[uuid.UUID]StreamSaver)

	for i := range cfg.Streams {
		validUUID, err := uuid.Parse(cfg.Streams[i].GUID)
		if err != nil {
			log.Printf("Not valid UUID: %s\n", cfg.Streams[i].GUID)
			continue
		}

		if cfg.Streams[i].RecordToRam && cfg.Streams[i].RamSize > 0 {
			streamSaver := StreamSaver{}
			streamSaver.ramStore = NewRamStore(cfg.Streams[i].RamSize)

			app.RecordApp.store[validUUID] = streamSaver
		}
	}

	for i := range app.RecordApp.store {
		go app.StartStreamSaver(i)
	}

}

func (app *Application) StartStreamSaver(streamID uuid.UUID) {

	quitCh := make(chan bool, 1)

	if app.existsWithType(streamID, "mse") {
		cuuid, ch, err := app.clientAdd(streamID)
		if err != nil {
			log.Printf("Can't add client for '%s' due the error: %s\n", streamID, err.Error())
			return
		}
		defer app.clientDelete(streamID, cuuid)

		//time.Sleep(time.Second * 10)

		for {
			select {
			case <-quitCh:
				app.RecordApp.store[streamID].ramStore.Clear()
				return
			case pck := <-ch:
				app.RecordApp.store[streamID].ramStore.AppendPkt(pck)
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
	timestamp, err := strconv.ParseInt(timestampStr, 10,64)
	if err != nil {
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	} else {
		timeT = time.Unix(0, timestamp)
	}

	ctx.Header("Cache-Control", "no-cache")
	pktArray, err := app.RecordApp.store[streamID].ramStore.FindPacket(timeT)
	if err != nil {
		log.Print(err)
		ctx.JSON(404, err.Error())
		return
	}

	log.Printf("Время разбора H264 %v", time.Since(start))

	codecData, err := app.codecGet(streamID)
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

	//decoder, err := ffmpeg.NewVideoDecoder(codecData[0])
	//if err != nil {
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	//
	//err = decoder.Setup()
	//if err != nil {
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	//
	//var img *ffmpeg.VideoFrame
	//for i := len(pktArray) - 1; i >= 0; i-- {
	//	img, err = decoder.Decode(pktArray[i].Pkt.Data)
	//	//if i != 0 {
	//	//	img.Free()
	//	//}
	//	//log.Print(i)
	//	//log.Printf("Время разбора H264 %v", time.Since(start))
	//	if err != nil {
	//		log.Print(err)
	//		ctx.JSON(404, err.Error())
	//		return
	//	}
	//}
	//
	//if img == nil {
	//	err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
	//	log.Print(err)
	//	ctx.JSON(404, err.Error())
	//	return
	//}
	//
	//converted := convertYcbCrImageToRgbaImage(img.Image)
	//img.Free()
	//
	//log.Printf("Время разбора H264 %v", time.Since(start))
	//
	//buf := new(bytes.Buffer)
	//jpeg.Encode(buf, converted, nil)
	//
	//log.Printf("Время разбора H264 %v", time.Since(start))
	//
	//ctx.Data(200, "image/jpeg", buf.Bytes())

	ctx.JSON(200,  pktArray)
	return
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
