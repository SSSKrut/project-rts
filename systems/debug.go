package systems

import "os"

// debugLog gates the chatty diagnostic prints (nav planner verdicts,
// spatial-bake dumps, macro replans). Chunk-IO error paths stay
// unconditional. Enable with RTS_DEBUG=1 (any non-empty value).
// One-shot probes keep their own env vars (RTS_NAV_PROBE).
var debugLog = os.Getenv("RTS_DEBUG") != ""
