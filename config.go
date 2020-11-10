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
	HTTPPort    string `json:"http_port"`
	HTTPTimeout int    `json:"http_timeout"`
}

type StreamST struct {
	URL       string `json:"url"`
	Status    bool   `json:"status"`
	Codecs    []av.CodecData
	Clients   map[string]viewer
	HlsChanel chan av.Packet
}
type viewer struct {
	c chan av.Packet
}

const (
	defaultHttpPort        = ":8083"
	defaultHlsDir          = "./hls"
	defaultHlsMsPerSegment = 10000
	defaultHlsCapacity     = 10
	defaultHlsWindowSize   = 5
)

func NewAppConfiguration(fname string) *ConfigST {
	var jsonConf ConfigST
	data, err := ioutil.ReadFile(fname)
	if err != nil {
		log.Fatalln(err)
	}
	err = json.Unmarshal(data, &jsonConf)
	if err != nil {
		log.Fatalln(err)
	}

	if jsonConf.Server.HTTPPort == "" {
		jsonConf.Server.HTTPPort = defaultHttpPort
	}

	if jsonConf.HlsDirectory == "" {
		jsonConf.HlsDirectory = defaultHlsDir
	}

	if jsonConf.HlsMsPerSegment == 0 {
		jsonConf.HlsMsPerSegment = defaultHlsMsPerSegment
	}

	if jsonConf.HlsCapacity == 0 {
		jsonConf.HlsCapacity = defaultHlsCapacity
	}

	if jsonConf.HlsWindowSize == 0 {
		jsonConf.HlsWindowSize = defaultHlsWindowSize
	}

	if jsonConf.HlsWindowSize > jsonConf.HlsCapacity {
		jsonConf.HlsWindowSize = jsonConf.HlsCapacity
	}

	for i, v := range jsonConf.Streams {
		v.Clients = make(map[string]viewer)
		v.HlsChanel = make(chan av.Packet, 100)
		jsonConf.Streams[i] = v
	}
	return &jsonConf
}

func (element *ConfigST) Cast(uuid string, pck av.Packet) {
	element.Streams[uuid].HlsChanel <- pck
	for _, v := range element.Streams[uuid].Clients {
		if len(v.c) < cap(v.c) {
			v.c <- pck
		}
	}
}

func (element *ConfigST) Ext(suuid string) bool {
	_, ok := element.Streams[suuid]
	return ok
}

func (element *ConfigST) CodecAdd(suuid string, codecs []av.CodecData) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	t := element.Streams[suuid]
	t.Codecs = codecs
	element.Streams[suuid] = t
}

func (element *ConfigST) CodecGet(suuid string) []av.CodecData {
	return element.Streams[suuid].Codecs
}

func (element *ConfigST) UpdateStatus(suuid string, status bool) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	t := element.Streams[suuid]
	t.Status = status
	element.Streams[suuid] = t
}

func (element *ConfigST) ClientAdd(suuid string) (string, chan av.Packet) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	cuuid := pseudoUUID()
	ch := make(chan av.Packet, 100)
	element.Streams[suuid].Clients[cuuid] = viewer{c: ch}
	return cuuid, ch
}

func (element *ConfigST) ClientDelete(suuid, cuuid string) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	delete(element.Streams[suuid].Clients, cuuid)
}

func (element *ConfigST) StartHlsCast(config *ConfigST, suuid string, stopCast chan bool) {
	defer element.mutex.Unlock()
	element.mutex.Lock()
	go startHls(config, suuid, element.Streams[suuid].HlsChanel, stopCast)
}

func (element *ConfigST) List() (string, []string) {
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
