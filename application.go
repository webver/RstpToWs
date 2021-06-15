package videoserver

import (
	"log"
	"sync"

	"github.com/gin-contrib/cors"

	"github.com/google/uuid"
	"github.com/webver/vdk/av"
)

// Application Configuration parameters for application
type Application struct {
	Server          *ServerInfo  `json:"server"`
	Streams         StreamsMap   `json:"streams"`
	HlsMsPerSegment int64        `json:"hls_ms_per_segment"`
	HlsDirectory    string       `json:"hls_directory"`
	HlsWindowSize   uint         `json:"hls_window_size"`
	HlsCapacity     uint         `json:"hls_window_capacity"`
	CorsConfig      *cors.Config `json:"-"`
	Mp4Directory    string       `json:"mp4_directory"`
	RecordApp       RecordApp    `json:"-"`
}

// ServerInfo Information about server
type ServerInfo struct {
	HTTPAddr      string `json:"http_addr"`
	VideoHTTPPort int    `json:"http_port"`
	APIHTTPPort   int    `json:"-"`
}

// StreamsMap Map wrapper for map[uuid.UUID]*StreamConfiguration with mutex for concurrent usage
type StreamsMap struct {
	sync.Mutex
	Streams map[uuid.UUID]*StreamConfiguration
}

func (sm *StreamsMap) getKeys() []uuid.UUID {
	sm.Lock()
	defer sm.Unlock()
	keys := make([]uuid.UUID, 0, len(sm.Streams))
	for k := range sm.Streams {
		keys = append(keys, k)
	}
	return keys
}

// StreamConfiguration Configuration parameters for stream
type StreamConfiguration struct {
	URL                  string   `json:"url"`
	Description          string   `json:"description"`
	Status               bool     `json:"status"`
	SupportedStreamTypes []string `json:"supported_stream_types"`
	OnDemand             bool     `json:"on_demand"`
	RunLock              bool     `json:"-"`
	Codecs               []av.CodecData
	Clients              map[uuid.UUID]Viewer
	closeGracefully      chan bool
}

type Viewer struct {
	c      chan av.Packet
	status chan bool
}

// NewApplication Prepare configuration for application
func NewApplication(cfg *ConfigurationArgs) (*Application, error) {
	tmp := Application{
		Server: &ServerInfo{
			HTTPAddr:      cfg.Server.HTTPAddr,
			VideoHTTPPort: cfg.Server.VideoHTTPPort,
			APIHTTPPort:   cfg.Server.APIHTTPPort,
		},
		Streams:         StreamsMap{Streams: make(map[uuid.UUID]*StreamConfiguration)},
		HlsMsPerSegment: cfg.HlsMsPerSegment,
		HlsDirectory:    cfg.HlsDirectory,
		HlsWindowSize:   cfg.HlsWindowSize,
		HlsCapacity:     cfg.HlsCapacity,
		Mp4Directory:    cfg.Mp4Directory,
	}
	if cfg.CorsConfig.UseCORS {
		tmp.setCors(&cfg.CorsConfig)
	}
	for i := range cfg.Streams {
		validUUID, err := uuid.Parse(cfg.Streams[i].GUID)
		if err != nil {
			log.Printf("Not valid UUID: %s\n", cfg.Streams[i].GUID)
			continue
		}
		tmp.Streams.Streams[validUUID] = &StreamConfiguration{
			URL:                  cfg.Streams[i].URL,
			Description:          cfg.Streams[i].Description,
			Clients:              make(map[uuid.UUID]Viewer),
			SupportedStreamTypes: cfg.Streams[i].StreamTypes,
			closeGracefully:      make(chan bool, 1),
		}
	}
	return &tmp, nil
}

func (app *Application) setCors(cfg *CorsConfiguration) {
	newCors := cors.DefaultConfig()
	app.CorsConfig = &newCors
	app.CorsConfig.AllowOrigins = cfg.AllowOrigins
	if len(cfg.AllowMethods) != 0 {
		app.CorsConfig.AllowMethods = cfg.AllowMethods
	}
	if len(cfg.AllowHeaders) != 0 {
		app.CorsConfig.AllowHeaders = cfg.AllowHeaders
	}
	app.CorsConfig.ExposeHeaders = cfg.ExposeHeaders
	app.CorsConfig.AllowCredentials = cfg.AllowCredentials
}

func (app *Application) castMSE(streamID uuid.UUID, pck av.Packet) error {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	curStream, ok := app.Streams.Streams[streamID]
	if !ok {
		return ErrStreamNotFound
	}
	for _, v := range curStream.Clients {
		//if len(v.c) < cap(v.c) {
		//	v.c <- pck
		//}
		select {
		case v.c <- pck:
			// message sent
		default:
			log.Print("Drop pkt")
			// message dropped
		}
	}
	return nil
}

func (app *Application) exists(streamID uuid.UUID) bool {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	_, ok := app.Streams.Streams[streamID]
	return ok

}

func (app *Application) existsWithType(streamID uuid.UUID, streamType string) bool {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	stream, ok := app.Streams.Streams[streamID]
	if !ok {
		return false
	}
	supportedTypes := stream.SupportedStreamTypes
	typeEnabled := typeExists(streamType, supportedTypes)
	return ok && typeEnabled
}

func (app *Application) codecAdd(streamID uuid.UUID, codecs []av.CodecData) {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	app.Streams.Streams[streamID].Codecs = codecs
}

func (app *Application) codecGet(streamID uuid.UUID) ([]av.CodecData, error) {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	curStream, ok := app.Streams.Streams[streamID]
	if !ok {
		return nil, ErrStreamNotFound
	}
	return curStream.Codecs, nil
}

func (app *Application) updateStatus(streamID uuid.UUID, status bool) error {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	t, ok := app.Streams.Streams[streamID]
	if !ok {
		return ErrStreamNotFound
	}
	t.Status = status
	app.Streams.Streams[streamID] = t

	for _, v := range t.Clients {
		select {
		case v.status <- status:
			// message sent
		default:
			// message dropped
		}
	}
	return nil
}

func (app *Application) getStatus(streamID uuid.UUID) (bool, error) {
	app.Streams.Lock()
	defer app.Streams.Unlock()
	t, ok := app.Streams.Streams[streamID]
	if !ok {
		return false, ErrStreamNotFound
	}

	return t.Status, nil
}

func (app *Application) clientAdd(streamID uuid.UUID) (uuid.UUID, *Viewer, error) {
	app.Streams.Lock()
	defer app.Streams.Unlock()

	clientID, err := uuid.NewUUID()
	if err != nil {
		return uuid.UUID{}, nil, err
	}
	ch := make(chan av.Packet, 100)
	statusCh := make(chan bool, 1)
	_, ok := app.Streams.Streams[streamID]
	if !ok {
		return uuid.UUID{}, nil, ErrStreamNotFound
	}
	viewer := Viewer{c: ch, status: statusCh}
	app.Streams.Streams[streamID].Clients[clientID] = viewer
	return clientID, &viewer, nil
}

func (app *Application) clientDelete(streamID, clientID uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	_, ok := app.Streams.Streams[streamID]
	if ok {
		close(app.Streams.Streams[streamID].Clients[clientID].c)
		close(app.Streams.Streams[streamID].Clients[clientID].status)
		delete(app.Streams.Streams[streamID].Clients, clientID)
	}

}

func (app *Application) list() (uuid.UUID, []uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	res := []uuid.UUID{}
	first := uuid.UUID{}
	for k := range app.Streams.Streams {
		if first.String() == "" {
			first = k
		}
		res = append(res, k)
	}
	return first, res
}

func (app *Application) runIFNotRun(uuid uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	if tmp, ok := app.Streams.Streams[uuid]; ok {
		if tmp.OnDemand && !tmp.RunLock {
			tmp.RunLock = true
			app.Streams.Streams[uuid] = tmp
			go app.StreamWorkerLoop(uuid)
		}
	}
}

func (app *Application) runUnlock(uuid uuid.UUID) {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	if tmp, ok := app.Streams.Streams[uuid]; ok {
		if tmp.OnDemand && tmp.RunLock {
			tmp.RunLock = false
			app.Streams.Streams[uuid] = tmp
		}
	}
}

func (app *Application) hasViewer(uuid uuid.UUID) bool {
	defer app.Streams.Unlock()
	app.Streams.Lock()
	if tmp, ok := app.Streams.Streams[uuid]; ok && len(tmp.Clients) > 0 {
		return true
	}
	return false
}
