package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"sync"

	"github.com/webver/vdk/av"
)

var Config = loadConfig()

type ConfigST struct {
	mutex           sync.Mutex
	Server          ServerST             `json:"server"`
	Streams         map[string]*StreamST `json:"streams"`
	HlsMsPerSegment int64                `json:"hlsMsPerSegment"`
	HlsDirectory    string               `json:"hlsDirectory"`
	HlsWindowSize   uint                 `json:"hlsWindowSize"`
	HlsCapacity     uint                 `json:"hlsWindowCapacity"`
}

type ServerST struct {
	HTTPPort string `json:"http_port"`
}

type StreamST struct {
	URL       string `json:"url"`
	Status    bool   `json:"status"`
	Codecs    []av.CodecData
	Clients   map[string]viewer
	HlsChanel chan av.Packet
	//HlsLastSegmentNumber int
}
type viewer struct {
	c chan av.Packet
}

const (
	defaultHlsDir          = "./hls"
	defaultHlsMsPerSegment = 10000
	defaultHlsCapacity     = 10
	defaultHlsWindowSize   = 5
)

func loadConfig() *ConfigST {
	var tmp ConfigST
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatalln(err)
	}
	err = json.Unmarshal(data, &tmp)
	if err != nil {
		log.Fatalln(err)
	}
	if tmp.HlsDirectory == "" {
		tmp.HlsDirectory = defaultHlsDir
	}

	if tmp.HlsMsPerSegment == 0 {
		tmp.HlsMsPerSegment = defaultHlsMsPerSegment
	}

	if tmp.HlsCapacity == 0 {
		tmp.HlsCapacity = defaultHlsCapacity
	}

	if tmp.HlsWindowSize == 0 {
		tmp.HlsWindowSize = defaultHlsWindowSize
	}

	if tmp.HlsWindowSize > tmp.HlsCapacity {
		tmp.HlsWindowSize = tmp.HlsCapacity
	}

	for i, v := range tmp.Streams {
		v.Clients = make(map[string]viewer)
		v.HlsChanel = make(chan av.Packet, 100)
		//v.HlsLastSegmentNumber = 0
		tmp.Streams[i] = v
	}
	return &tmp
}

func (element *ConfigST) cast(uuid string, pck av.Packet) {
	element.Streams[uuid].HlsChanel <- pck
	for _, v := range element.Streams[uuid].Clients {
		if len(v.c) < cap(v.c) {
			v.c <- pck
		}
	}
}

func (element *ConfigST) ext(suuid string) bool {
	_, ok := element.Streams[suuid]
	return ok
}

func (element *ConfigST) codecAdd(suuid string, codecs []av.CodecData) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	t := element.Streams[suuid]
	t.Codecs = codecs
	element.Streams[suuid] = t
}

func (element *ConfigST) codecGet(suuid string) []av.CodecData {
	return element.Streams[suuid].Codecs
}

func (element *ConfigST) updateStatus(suuid string, status bool) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	t := element.Streams[suuid]
	t.Status = status
	element.Streams[suuid] = t
}

func (element *ConfigST) clientAdd(suuid string) (string, chan av.Packet) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	cuuid := pseudoUUID()
	ch := make(chan av.Packet, 100)
	element.Streams[suuid].Clients[cuuid] = viewer{c: ch}
	return cuuid, ch
}

func (element *ConfigST) clientDelete(suuid, cuuid string) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	delete(element.Streams[suuid].Clients, cuuid)
}

func (element *ConfigST) startHlsCast(suuid string, stopCast chan bool) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	go startHls(suuid, element.Streams[suuid].HlsChanel, stopCast)
}

//func (element *ConfigST) getLastHlsSegmentNumber(suuid string) int {
//	return element.Streams[suuid].HlsLastSegmentNumber
//}
//
//func (element *ConfigST) updateLastHlsSegmentNumber(suuid string, lastSegmentNumber int)  {
//	defer element.mutex.Unlock()
//	element.mutex.Lock()
//	element.Streams[suuid].HlsLastSegmentNumber = lastSegmentNumber
//}

func (element *ConfigST) list() (string, []string) {
	var res []string
	var fist string
	for k := range element.Streams {
		if fist == "" {
			fist = k
		}
		res = append(res, k)
	}
	return fist, res
}

func pseudoUUID() (uuid string) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}
	uuid = fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return
}
