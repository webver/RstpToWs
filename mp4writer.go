package videoserver

import (
	"fmt"
	"github.com/LdDl/vdk/av"
	"github.com/LdDl/vdk/format/mp4"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"log"
	"os"
	"path/filepath"
	"time"
)

type Mp4Writer struct{
	streamID uuid.UUID
	maxFileSize int //максимальный размер файла
	codecData av.CodecData

	startTime time.Time //Время 1ого полученного пакета
	lastPacketTime time.Duration //Время последнего записанного пакета

	rxPktNumber uint //количество пакетов в файле
	fileSize int	//размер файла

	mp4Muxer *mp4.Muxer

	folderPath string
	isConnected bool

	lastKeyFrame av.Packet

	segmentStore *SegmentStore
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

	return &mp4Writer, nil
}

func  (mp4Writer *Mp4Writer) FindInFile(segmentPath string, timeInFile time.Duration) ([]av.CodecData, []av.Packet, error) {
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
		pktArray = append(pktArray, pkt)
		if mp4Demuxer.CurrentTime() >= timeInFile {
			return codecData, pktArray, err
		}
	}
}

func (mp4Writer *Mp4Writer) startMp4Writer(codecData []av.CodecData, ch chan av.Packet, stopCast chan bool) error {

	mp4Writer.isConnected = true

	for mp4Writer.isConnected {
		// Create new segment file
		segmentName := fmt.Sprintf("%d.mp4", time.Now().Unix())
		segmentPath := filepath.Join(mp4Writer.folderPath, segmentName)
		outFile, err := os.Create(segmentPath)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't create mp4-segment for stream %s", mp4Writer.streamID))
		}
		mp4Muxer := mp4.NewMuxer(outFile)

		// Write header
		if err := mp4Muxer.WriteHeader(codecData); err != nil {
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
			mp4Writer.startTime = time.Now().Add(-mp4Writer.lastKeyFrame.Time)
			if err = mp4Muxer.WritePacket(mp4Writer.lastKeyFrame); err != nil {
				return errors.Wrap(err, fmt.Sprintf("Can't write packet for mp4 muxer for stream %s (1)", mp4Writer.streamID))
			}

			mp4Writer.lastPacketTime = mp4Writer.lastKeyFrame.Time
		}

	segmentLoop:
		for {
			select {
			case <-stopCast:
				mp4Writer.isConnected = false
				break segmentLoop
			case pck := <-ch:
				if pck.Idx == videoStreamIdx && pck.IsKeyFrame {
					if isStarted == false {
						mp4Writer.startTime = time.Now().Add(-pck.Time)
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
				if (pck.Idx == videoStreamIdx && pck.Time > mp4Writer.lastPacketTime) || pck.Idx != videoStreamIdx {
					if err = mp4Muxer.WritePacket(pck); err != nil {
						return errors.Wrap(err, fmt.Sprintf("Can't write packet for mp4 muxer for stream %s (2)", mp4Writer.streamID))
					}
					if pck.Idx == videoStreamIdx {
						mp4Writer.fileSize += len(pck.Data)
						mp4Writer.lastPacketTime = pck.Time
					}
				} else {
					// fmt.Println("Current packet time < previous ")
				}
			}
		}

		if err := mp4Muxer.WriteTrailer(); err != nil {
			log.Printf("Can't write trailing data for TS muxer for %s: %s\n", mp4Writer.streamID, err.Error())
		}

		if err := outFile.Close(); err != nil {
			log.Printf("Can't close file %s: %s\n", outFile.Name(), err.Error())
		}

		//Update segment data
		err = mp4Writer.segmentStore.AppendSegment(segmentPath, SegmentData{
			startTime : mp4Writer.startTime,
			endTime: mp4Writer.startTime.Add(mp4Writer.lastPacketTime),
		})
		if err != nil {
			log.Printf("Can't append segment to store %s: %s\n", segmentPath, err.Error())
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
	segmentFiles, err := filepath.Glob(filepath.Join(mp4Writer.folderPath, fmt.Sprintf("%s*.ts", mp4Writer.streamID)))
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

func (mp4Writer *Mp4Writer) initSegmentStore() error {
	// Find possible segment files in current directory
	segmentFiles, err := filepath.Glob(filepath.Join(mp4Writer.folderPath, fmt.Sprintf("%s*.ts", mp4Writer.streamID)))
	if err != nil {
		log.Printf("Can't find glob for '%s': %s\n", mp4Writer.streamID, err.Error())
		return err
	}
	for _, segmentFile := range segmentFiles {
		_, fileName := filepath.Split(segmentFile)

		outFile, err := os.Open(segmentFile)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Can't open mp4-segment for stream %s", mp4Writer.streamID))
		}

		mp4Demuxer := mp4.NewDemuxer(outFile)

		mp4Demuxer.Streams()

		if _, ok := mp4Writer.segmentStore.segmentDataMap[fileName]; !ok {
			if err := os.Remove(segmentFile); err != nil {
				log.Printf("Can't call removeOutdatedSegments() for segment %s: %s\n", segmentFile, err.Error())
			}
		}

		outFile.Close()

	}
	return nil
}