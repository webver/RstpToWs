package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/grafov/m3u8"
	"github.com/webver/vdk/av"
	"github.com/webver/vdk/format/ts"
)

func startHls(suuid string, ch chan av.Packet, stopCast chan bool) {
	// create hls playlist
	playlistFileName := filepath.Join(Config.HlsDirectory, fmt.Sprintf("%s.m3u8", suuid))
	playlist, err := m3u8.NewMediaPlaylist(Config.HlsWindowSize, Config.HlsCapacity)
	if err != nil {
		log.Println(err)
		return
	}

	quit := make(chan bool)
	i := 0
	var lastPacketTime time.Duration = 0
fileLoop:
	for {
		select {
		case <-quit:
			break fileLoop
		default:
			// create new segment file
			segmentName := fmt.Sprintf("%s%04d.ts", suuid, i)
			segmentPath := filepath.Join(Config.HlsDirectory, segmentName)
			outFile, err := os.Create(segmentPath)
			if err != nil {
				//handleError(streamLogger, conn, err)
				return
			}
			tsMuxer := ts.NewMuxer(outFile)

			// write header
			if err := tsMuxer.WriteHeader(Config.codecGet(suuid)); err != nil {
				//handleError(streamLogger, conn, err)
				return
			}

			// write packets
			var segmentLength time.Duration = 0
			var packetLength time.Duration = 0
			//var start bool

		segmentLoop:
			for segmentLength.Milliseconds() < Config.HlsMsPerSegment {
				select {
				case <-stopCast:
					quit <- true
					break segmentLoop
				case pck := <-ch:
					//if pck.IsKeyFrame {
					//	start = true
					//}
					//if !start {
					//	continue
					//}
					// write packet to destination
					if err = tsMuxer.WritePacket(pck); err != nil {
						log.Println("Ts muxer write error")
						return
					}
					// calculate segment length
					packetLength = pck.Time - lastPacketTime
					segmentLength += packetLength
					lastPacketTime = pck.Time
				}
			}
			// write trailer
			if err := tsMuxer.WriteTrailer(); err != nil {
				log.Println(err)
				return
			}

			// close segment file
			if err := outFile.Close(); err != nil {
				log.Println(err)
				return
			}
			log.Printf("Wrote segment %s\n", segmentName)

			// update playlist
			playlist.Slide(segmentName, segmentLength.Seconds(), "")
			playlistFile, err := os.Create(playlistFileName)
			if err != nil {
				log.Println(err)
				return
			}
			playlistFile.Write(playlist.Encode().Bytes())
			playlistFile.Close()

			// cleanup segments
			if err := removeOutdatedSegments(suuid, playlist); err != nil {
				log.Println(err)
				return
			}

			// increase segment index
			i++
		}
	}

	filesToRemove := make([]string, len(playlist.Segments)+1)

	// collect obsolete files
	for _, segment := range playlist.Segments {
		if segment != nil {
			filesToRemove = append(filesToRemove, segment.URI)
		}
	}
	filesToRemove = append(filesToRemove, playlistFileName)

	// delete them later
	go func(delay time.Duration, filesToRemove []string) {
		log.Printf("Files to be deleted after %v: %v\n", delay, filesToRemove)
		time.Sleep(delay)
		for _, file := range filesToRemove {
			if file != "" {
				if err := os.Remove(filepath.Join(Config.HlsDirectory, file)); err != nil {
					log.Println(err)
				} else {
					log.Printf("Successfully removed %s\n", file)
				}
			}
		}
	}(time.Duration(Config.HlsMsPerSegment*int64(playlist.Count()))*time.Millisecond, filesToRemove)
}

func removeOutdatedSegments(suuid string, playlist *m3u8.MediaPlaylist) error {
	// write all playlist segment URIs into map
	currentSegments := make(map[string]struct{}, len(playlist.Segments))
	for _, segment := range playlist.Segments {
		if segment != nil {
			currentSegments[segment.URI] = struct{}{}
		}
	}
	// find (probably) segment files in current directory
	segmentFiles, err := filepath.Glob(filepath.Join(Config.HlsDirectory, fmt.Sprintf("%s*.ts", suuid)))
	if err != nil {
		return err
	}
	for _, segmentFile := range segmentFiles {
		_, fileName := filepath.Split(segmentFile)
		// check if file belongs to a playlist segment
		if _, ok := currentSegments[fileName]; !ok {
			if err := os.Remove(segmentFile); err != nil {
				log.Println(err)
			} else {
				log.Printf("Removed segment %s\n\n", segmentFile)
			}
		}
	}
	return nil
}
