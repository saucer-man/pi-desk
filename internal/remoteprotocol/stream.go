package remoteprotocol

const (
	MethodStreamCredit    = "stream.credit"
	MethodStreamData      = "stream.data"
	MethodProcessAccepted = "process.accepted"
	MethodTerminalInput   = "terminal.input"
	MethodTerminalResize  = "terminal.resize"
	MaxStreamChunkBytes   = 256 << 10
)

type StreamCredit struct {
	Bytes uint32 `json:"bytes"`
}

type StreamData struct {
	Stream   string `json:"stream"`
	Sequence uint64 `json:"sequence"`
}

type ProcessAccepted struct {
	ProcessID string `json:"processId"`
}

type TerminalResize struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}
