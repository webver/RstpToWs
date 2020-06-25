package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/grafov/m3u8"
	"github.com/webver/vdk/av"
	"github.com/webver/vdk/format/ts"
)

func ensureDir(dirName string) error {
	err := os.MkdirAll(dirName, 0777)

	if err == nil || os.IsExist(err) {
		return nil
	} else {
		return err
	}
}

func startHls(suuid string, ch chan av.Packet, stopCast chan bool) {
	err := ensureDir(Config.HlsDirectory)
	if err != nil {
		panic("Wrong hls dir path")
	}

	// create hls playlist
	playlistFileName := filepath.Join(Config.HlsDirectory, fmt.Sprintf("%s.m3u8", suuid))
	playlist, err := m3u8.NewMediaPlaylist(Config.HlsWindowSize, Config.HlsCapacity)
	if err != nil {
		fmt.Println(err)
	}

	isConnected := true
	//segmentNumber := Config.getLastHlsSegmentNumber(suuid)
	segmentNumber := 0
	var lastPacketTime time.Duration = 0
	var lastKeyFrame av.Packet

	for isConnected {
		// create new segment file
		segmentName := fmt.Sprintf("%s%04d.ts", suuid, segmentNumber)
		segmentPath := filepath.Join(Config.HlsDirectory, segmentName)
		outFile, err := os.Create(segmentPath)
		if err != nil {
			fmt.Println(err)
		}
		tsMuxer := ts.NewMuxer(outFile)

		// write header
		if err := tsMuxer.WriteHeader(Config.codecGet(suuid)); err != nil {
			fmt.Println(err)
		}

		var videoStreamIdx int8 = 0
		for idx, codec := range Config.codecGet(suuid) {
			if codec.Type().IsVideo() == true {
				videoStreamIdx = int8(idx)
				break
			}
		}

		// write packets
		var segmentLength time.Duration = 0
		var packetLength time.Duration = 0
		var segmentCount int = 0
		var start = false

		//write lastKeyFrame if exist
		if lastKeyFrame.IsKeyFrame == true {
			start = true
			if err = tsMuxer.WritePacket(lastKeyFrame); err != nil {
				fmt.Println("Ts muxer write error")
			}
			// calculate segment length
			packetLength = lastKeyFrame.Time - lastPacketTime
			lastPacketTime = lastKeyFrame.Time
			segmentLength += packetLength
			segmentCount++
		}

	segmentLoop:
		for {
			select {
			case <-stopCast:
				isConnected = false
				break segmentLoop
			case pck := <-ch:
				if pck.Idx == videoStreamIdx && pck.IsKeyFrame {
					start = true
					if segmentLength.Milliseconds() >= Config.HlsMsPerSegment {
						lastKeyFrame = pck
						break segmentLoop
					}
				}
				if !start {
					continue
				}
				if (pck.Idx == videoStreamIdx && pck.Time > lastPacketTime) || pck.Idx != videoStreamIdx {
					//write packet to destination
					if err = tsMuxer.WritePacket(pck); err != nil {
						fmt.Println("Ts muxer write error", err)
					}
					if pck.Idx == videoStreamIdx {
						// calculate segment length
						packetLength = pck.Time - lastPacketTime
						lastPacketTime = pck.Time
						segmentLength += packetLength
					}
					segmentCount++
				} else {
					fmt.Println("Current packet time < previous ")
				}
			}
		}
		// write trailer
		if err := tsMuxer.WriteTrailer(); err != nil {
			fmt.Println(err)
		}

		// close segment file
		if err := outFile.Close(); err != nil {
			fmt.Println(err)
		}
		fmt.Printf("Wrote segment %s %f %d\n", segmentName, segmentLength.Seconds(), segmentCount)

		// update playlist
		playlist.Slide(segmentName, segmentLength.Seconds(), "")
		playlistFile, err := os.Create(playlistFileName)
		if err != nil {
			fmt.Println(err)
		}
		playlistFile.Write(playlist.Encode().Bytes())
		playlistFile.Close()

		// cleanup segments
		if err := removeOutdatedSegments(suuid, playlist); err != nil {
			fmt.Println(err)
		}

		// increase segment index
		segmentNumber++
	}

	filesToRemove := make([]string, len(playlist.Segments)+1)

	// collect obsolete files
	for _, segment := range playlist.Segments {
		if segment != nil {
			filesToRemove = append(filesToRemove, segment.URI)
		}
	}
	_, fileName := filepath.Split(playlistFileName)
	filesToRemove = append(filesToRemove, fileName)

	// delete them later
	go func(delay time.Duration, filesToRemove []string) {
		fmt.Printf("Files to be deleted after %v: %v\n", delay, filesToRemove)
		time.Sleep(delay)
		for _, file := range filesToRemove {
			if file != "" {
				if err := os.Remove(filepath.Join(Config.HlsDirectory, file)); err != nil {
					fmt.Println(err)
				} else {
					fmt.Printf("Successfully removed %s\n", file)
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
				fmt.Println(err)
			} else {
				fmt.Printf("Removed segment %s\n", segmentFile)
			}
		}
	}
	return nil
}
