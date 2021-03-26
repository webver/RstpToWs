package videoserver

import (
	"fmt"
)

var (
	// ErrStreamNotFound When map of streams doesn't contain requested key
	ErrStreamNotFound              = fmt.Errorf("Stream not found for provided ID")
	ErrorStreamExitNoVideoOnStream = fmt.Errorf("Stream Exit No Video On Stream")
	ErrorStreamExitRtspDisconnect  = fmt.Errorf("Stream Exit Rtsp Disconnect")
	ErrorStreamExitNoViewer        = fmt.Errorf("Stream Exit On Demand No Viewer")
)
