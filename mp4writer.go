package videoserver

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/webver/vdk/av"
	"github.com/webver/vdk/format/mp4"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Mp4Writer struct {
	streamID    uuid.UUID
	folderPath  string //Путь к папке
	maxFileSize int    //максимальный размер файла
	codecData   av.CodecData

	segmentStore *SegmentStore

	//Текущее состояние
	startTime       time.Time     //Время 1ого полученного пакета после подключения
	firstPacketTime time.Time     //Время 1ого полученного пакета для сегмента
	lastPacketTime  time.Duration //Время последнего записанного пакета  для сегмента

	//Нужно только внутри цикла
	fileSize     int //размер файла
	isConnected  bool
	lastKeyFrame av.Packet
}

func NewMp4Writer(mp4Directory string, streamID uuid.UUID, maxFileSize int, maxSegmentCount int) (*Mp4Writer, error) {
	mp4Writer := Mp4Writer{}

	mp4Writer.streamID = streamID
	mp4Writer.maxFileSize = maxFileSize

	mp4Writer.folderPath = filepath.Join(mp4Directory, streamID.String())
	err := ensureDir(mp4Writer.folderPath)
	if err != nil {
		return nil, errors.Wrap(err, "Can't create directory for HLS temporary files")
	}

	mp4Writer.segmentStore = NewSegmentStore(maxSegmentCount)
	err = mp4Writer.initSegmentStore()
	if err != nil {
		return nil, errors.Wrap(err, "Can't init segment store with old files")
	}

	return &mp4Writer, nil
}

func (mp4Writer *Mp4Writer) FindInFile(fileName string, timeInFile time.Duration) ([]av.CodecData, []av.Packet, error) {
	segmentPath := filepath.Join(mp4Writer.folderPath, fileName)
	outFile, err := os.Open(segmentPath)
	if err != nil {
		return nil, nil, errors.Wrap(err, fmt.Sprintf("Can't open mp4-segment for stream %s", mp4Writer.streamID))
	}
	defer outFile.Close()

	mp4Demuxer := mp4.NewDemuxer(outFile)

	codecData, err := mp4Demuxer.Streams()
	if err != nil {
		return nil, nil, errors.Wrap(err, fmt.Sprintf("No codec data for mp4-segment for stream %s", mp4Writer.streamID))
	}

	err = mp4Demuxer.SeekToTime(timeInFile)
	if err != nil {
		return nil, nil, errors.Wrap(err, fmt.Sprintf("Can't seek mp4-segment for stream %s", mp4Writer.streamID))
	}

	pktArray := make([]av.Packet, 0)
	for {
		pkt, err := mp4Demuxer.ReadPacket()
		if err != nil {
			return nil, nil, errors.Wrap(err, fmt.Sprintf("Cant read pkt from mp4-segment for stream %s", mp4Writer.streamID))
		}

		if mp4Demuxer.CurrentTime() > timeInFile {
			return codecData, pktArray, err
		} else {
			pktArray = append(pktArray, pkt)
		}
	}
}

func (mp4Writer *Mp4Writer) startMp4Writer(codecData []av.CodecData, ch chan av.Packet, stopCast chan bool) error {

	mp4Writer.lastPacketTime = 0
	mp4Writer.lastKeyFrame = av.Packet{}
	mp4Writer.fileSize = 0
	mp4Writer.isConnected = true

	for mp4Writer.isConnected {
		// Create new segment file
		err := mp4Writer.removeTmpFiles()
		if err != nil {
			log.Printf("Can't remove tmp files for stream %s: %s\n", mp4Writer.streamID, err.Error())
		}

		segmentTimestamp := time.Now().Unix()
		segmentTmpName := fmt.Sprintf("%d.tmp", segmentTimestamp)
		segmentTempPath := filepath.Join(mp4Writer.folderPath, segmentTmpName)
		outFile, err := os.Create(segmentTempPath)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't create mp4-segment for stream %s", mp4Writer.streamID))
		}
		mp4Muxer := mp4.NewMuxer(outFile)

		// Write header
		if err = mp4Muxer.WriteHeader(codecData); err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't write header for mp4 muxer for stream %s", mp4Writer.streamID))
		}

		// Write packets
		videoStreamIdx := int8(0)
		for idx, codec := range codecData {
			if codec.Type().IsVideo() {
				videoStreamIdx = int8(idx)
				break
			}
		}

		isStarted := false
		mp4Writer.fileSize = 0

		// Write lastKeyFrame if exist
		if mp4Writer.lastKeyFrame.IsKeyFrame {
			isStarted = true
			now := time.Now().UTC()
			//mp4Writer.startTime = now.Add(-mp4Writer.lastKeyFrame.Time)
			mp4Writer.firstPacketTime = now
			if err = mp4Muxer.WritePacket(mp4Writer.lastKeyFrame); err != nil {
				return errors.Wrap(err, fmt.Sprintf("Can't write packet for mp4 muxer for stream %s (1)", mp4Writer.streamID))
			}

			mp4Writer.lastPacketTime = mp4Writer.lastKeyFrame.Time
		}

	segmentLoop:
		for {
			select {
			case status := <-stopCast:
				if status == false {
					mp4Writer.isConnected = false
					break segmentLoop
				}
			case pck, ok := <-ch:
				if !ok {
					mp4Writer.isConnected = false
					break segmentLoop
				}
				if pck.Idx == videoStreamIdx && pck.IsKeyFrame {
					if isStarted == false {
						now := time.Now().UTC()
						mp4Writer.startTime = now.Add(-pck.Time).Add(-pck.CompositionTime)
						mp4Writer.firstPacketTime = now
					}
					isStarted = true

					if mp4Writer.fileSize >= mp4Writer.maxFileSize {
						mp4Writer.lastKeyFrame = pck
						break segmentLoop
					}
				}
				if !isStarted {
					continue
				}
				if (pck.Idx == videoStreamIdx && (pck.Time+pck.CompositionTime) > mp4Writer.lastPacketTime) || pck.Idx != videoStreamIdx {
					if err = mp4Muxer.WritePacket(pck); err != nil {
						return errors.Wrap(err, fmt.Sprintf("Can't write packet for mp4 muxer for stream %s (2)", mp4Writer.streamID))
					}
					if pck.Idx == videoStreamIdx {
						mp4Writer.fileSize += len(pck.Data)
						mp4Writer.lastPacketTime = pck.Time + pck.CompositionTime
					}
				} else {
					log.Println("Current packet time < previous ", mp4Writer.lastPacketTime, pck.Time, pck.CompositionTime)
					if mp4Writer.lastPacketTime-pck.Time > time.Second*10 {
						mp4Writer.isConnected = false
						break segmentLoop
					}
				}
			}
		}

		if err = mp4Muxer.WriteTrailerWithStartTime(mp4Writer.firstPacketTime); err != nil {
			log.Printf("Can't write trailing data for Mp4 muxer for %s: %s\n", mp4Writer.streamID, err.Error())
		}

		if err = outFile.Close(); err != nil {
			log.Printf("Can't close file %s: %s\n", outFile.Name(), err.Error())
		}

		segmentName := fmt.Sprintf("%d.mp4", segmentTimestamp)
		segmentPath := filepath.Join(mp4Writer.folderPath, segmentName)

		err = os.Rename(segmentTempPath, segmentPath)
		if err != nil {
			log.Printf("Can't rename segment to store %s: %s\n", segmentTmpName, err.Error())
		}

		//Update segment data
		err = mp4Writer.segmentStore.AppendSegment(segmentName, SegmentData{
			startTime: mp4Writer.firstPacketTime.UTC(),
			endTime:   mp4Writer.startTime.UTC().Add(mp4Writer.lastPacketTime),
		})
		if err != nil {
			log.Printf("Can't append segment to store %s: %s\n", segmentName, err.Error())
		}

		// Cleanup segments
		if err := mp4Writer.removeOutdatedSegments(); err != nil {
			log.Printf("Can't call removeOutdatedSegments on stream %s: %s\n", mp4Writer.streamID, err.Error())
		}
	}

	return nil
}

func (mp4Writer *Mp4Writer) removeOutdatedSegments() error {
	// Find possible segment files in current directory
	segmentFiles, err := filepath.Glob(filepath.Join(mp4Writer.folderPath, "*.mp4"))
	if err != nil {
		log.Printf("Can't find glob for '%s': %s\n", mp4Writer.streamID, err.Error())
		return err
	}
	for _, segmentFile := range segmentFiles {
		_, fileName := filepath.Split(segmentFile)
		// Check if file belongs to a playlist's segment
		if _, ok := mp4Writer.segmentStore.segmentDataMap[fileName]; !ok {
			if err := os.Remove(segmentFile); err != nil {
				log.Printf("Can't call removeOutdatedSegments() for segment %s: %s\n", segmentFile, err.Error())
			}
		}
	}
	return nil
}

func (mp4Writer *Mp4Writer) removeTmpFiles() error {
	// Find possible segment files in current directory
	tmpFiles, err := filepath.Glob(filepath.Join(mp4Writer.folderPath, "*.tmp"))
	if err != nil {
		log.Printf("Can't find tmp files for '%s': %s\n", mp4Writer.streamID, err.Error())
		return err
	}
	for _, tmpFile := range tmpFiles {
		if err := os.Remove(tmpFile); err != nil {
			log.Printf("Can't call removeTmpFiles() for file %s: %s\n", tmpFile, err.Error())
		}
	}
	return nil
}

func (mp4Writer *Mp4Writer) initSegmentStore() error {
	// Find possible segment files in current directory
	segmentFiles, err := filepath.Glob(filepath.Join(mp4Writer.folderPath, "*.mp4"))
	if err != nil {
		log.Printf("Can't find glob for '%s': %s\n", mp4Writer.streamID, err.Error())
		return err
	}

	segmentFileTimestamps := make([]int, 0, len(segmentFiles))
	for _, segmentFile := range segmentFiles {
		_, fileName := filepath.Split(segmentFile)
		var timestamp int
		n, err := fmt.Sscanf(fileName, "%d.mp4", &timestamp)
		if err != nil || n != 1 {
			log.Print("Не правильный формат навания файла")
		} else {
			segmentFileTimestamps = append(segmentFileTimestamps, timestamp)
		}
	}

	sort.Slice(segmentFileTimestamps, func(i, j int) bool {
		return segmentFileTimestamps[i] < segmentFileTimestamps[j]
	})

	for _, segmentFileTimestamp := range segmentFileTimestamps {
		fileName := fmt.Sprintf("%d.mp4", segmentFileTimestamp)
		segmentPath := filepath.Join(mp4Writer.folderPath, fileName)

		outFile, err := os.Open(segmentPath)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't open mp4-segment for stream %s", mp4Writer.streamID))
		}

		mp4Demuxer := mp4.NewDemuxer(outFile)

		movieInfo, err := mp4Demuxer.GetMovieHeader()
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't get movie info from mp4-segment for stream %s", mp4Writer.streamID))
		}

		err = mp4Writer.segmentStore.AppendSegment(fileName, SegmentData{
			startTime: movieInfo.CreateTime,
			endTime:   movieInfo.CreateTime.Add(time.Duration(movieInfo.Duration) * time.Second / time.Duration(movieInfo.TimeScale)),
		})
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't append segment data for stream %s", mp4Writer.streamID))
		}

		err = outFile.Close()
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't close file for stream %s", mp4Writer.streamID))
		}

	}
	return nil
}

//
//func archiveWrapper(ctx *gin.Context, app *Application) {
//
//}
//
//
//func archiveWrapper(ctx *gin.Context, app *Application) {
//	start := time.Now()
//
//	cuuid := ctx.Param("suuid")
//	streamID, err := uuid.Parse(uuidRegExp.FindString(cuuid))
//	if err != nil {
//		log.Print(err)
//		ctx.JSON(404, err.Error())
//		return
//	}
//
//	timeT := time.Now().Add(time.Second * time.Duration(-5))
//
//	timestampStr := ctx.Query("timestamp")
//	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
//	if err != nil {
//		log.Print(err)
//		ctx.JSON(404, err.Error())
//		return
//	} else {
//
//		timeT = time.Unix(timestamp, 0)
//		msStr := ctx.Query("ms")
//		ms, err := strconv.ParseInt(msStr, 10, 64)
//		if err != nil {
//			log.Print(err)
//			ctx.JSON(404, err.Error())
//			return
//		} else {
//			timeT = time.Unix(timestamp, ms*1000000)
//		}
//	}
//
//	ctx.Header("Cache-Control", "no-cache")
//
//	var pktArray []av.Packet
//	var codecData []av.CodecData
//
//	measureStart := time.Now()
//
//	app.RecordApp.m.Lock()
//	streamSaver, ok := app.RecordApp.store[streamID]
//	app.RecordApp.m.Unlock()
//	if !ok {
//		err = errors.New(fmt.Sprintf("Поток %s не найден", streamID.String()))
//		log.Print(err)
//		ctx.JSON(404, err.Error())
//		return
//	}
//
//	if timeT.After(streamSaver.ramStore.firstPktTime) && timeT.Before(streamSaver.ramStore.lastPktTime) {
//		pktArray, err = streamSaver.ramStore.FindPacket(timeT)
//		if err != nil {
//			log.Print(err)
//			ctx.JSON(404, err.Error())
//			return
//		}
//
//		log.Printf("Время разбора H264 %v", time.Since(measureStart))
//		log.Printf("Пакеты найдены в памяти")
//
//		codecData, err = app.codecGet(streamID)
//		if err != nil {
//			log.Printf("Can't add client '%s' due the error: %s\n", streamID, err.Error())
//			ctx.JSON(404, err.Error())
//			return
//		}
//		if codecData == nil || len(codecData) == 0 || codecData[0].Type() != av.H264 {
//			log.Printf("No codec information for stream %s\n", streamID)
//			ctx.JSON(404, err.Error())
//			return
//		}
//	} else {
//		fileName, segmentStartTime, err := streamSaver.mp4writer.segmentStore.FindSegment(timeT)
//		if err != nil {
//			log.Print(err)
//			ctx.JSON(404, err.Error())
//			return
//		}
//
//		timeInFile := timeT.Sub(segmentStartTime)
//
//		codecData, pktArray, err = streamSaver.mp4writer.FindInFile(fileName, timeInFile)
//		if err != nil {
//			log.Print(err)
//			ctx.JSON(404, err.Error())
//			return
//		}
//
//		log.Printf("Пакеты найдены на диске")
//	}
//
//	if codecData == nil || len(codecData) == 0 || codecData[0].Type() != av.H264 {
//		log.Printf("No codec information for stream %s\n", streamID)
//		ctx.JSON(404, err.Error())
//		return
//	}
//
//	log.Printf("Время получения пакетов %v", time.Since(measureStart))
//
//	log.Printf("Время получения изображения суммарно %v", time.Since(start))
//	ctx.JSON(200, pktArray)
//
//	//decoder, err := ffmpeg.NewVideoDecoder(codecData[0])
//	//if err != nil {
//	//	log.Print(err)
//	//	ctx.JSON(404, err.Error())
//	//	return
//	//}
//	//
//	//measureStart = time.Now()
//	//
//	//var img *ffmpeg.VideoFrame
//	////for i := range pktArray {
//	////err = decoder.DecoderAppendPkt(pktArray[i].Data)
//	////if err != nil {
//	////	log.Print(err)
//	////	ctx.JSON(404, err.Error())
//	////	return
//	////}
//	////
//	////img, err = decoder.DecoderReceiveFrame()
//	//img, err = decoder.DecodeNewApi(pktArray)
//	//if err != nil {
//	//	err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
//	//	log.Print(err)
//	//	ctx.JSON(404, err.Error())
//	//	return
//	//}
//	////if i != len(pktArray) -1 {
//	////	img.Free()
//	////}
//	////}
//	//
//	//log.Printf("Время декодирования %v", time.Since(measureStart))
//	//measureStart = time.Now()
//	//
//	//decoder.Free()
//	//
//	//log.Printf("Время получения изображения %v", time.Since(measureStart))
//	//measureStart = time.Now()
//	//
//	//if img == nil {
//	//	err = errors.New(fmt.Sprintf("Не возможно сформировать картинку для времени %v", timeT))
//	//	log.Print(err)
//	//	ctx.JSON(404, err.Error())
//	//	return
//	//}
//	//
//	//buf := new(bytes.Buffer)
//	//err = jpeg.Encode(buf, &img.Image, &jpeg.EncoderOptions{Quality: 90})
//	//if err != nil {
//	//	err = errors.New(fmt.Sprintf("Ошибка преобразования в jpeg для времени %v", timeT))
//	//	log.Print(err)
//	//	ctx.JSON(404, err.Error())
//	//	return
//	//}
//	//
//	//img.Free()
//	//
//	//log.Printf("Время преобразования в JPEG %v", time.Since(measureStart))
//	//log.Printf("Время получения изображения суммарно %v", time.Since(start))
//	//
//	//ctx.Data(200, "image/jpeg", buf.Bytes())
//
//	return
//}

type CameraArchive struct {
	Id          uuid.UUID     `json:"id"`
	URL         string        `json:"url"`
	Description string        `json:"description"`
	Files       []SegmentInfo `json:"files"`
}

func (app *Application) cameraArchiveWrapper() []CameraArchive {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	res := make([]CameraArchive, 0)

	for k, v := range app.Streams.Streams {

		cameraArchive := CameraArchive{
			Id:          k,
			URL:         v.URL,
			Description: v.Description,
			Files:       app.RecordApp.store[k].mp4writer.segmentStore.GetSegmentList(),
		}

		res = append(res, cameraArchive)
	}
	return res
}
