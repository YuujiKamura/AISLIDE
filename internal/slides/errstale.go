package slides

import "errors"

// ErrStale: Apply の writeControl.requiredRevisionId が現在の revision と一致しない
// （FAILED_PRECONDITION）ときに Apply が返す（errors.Is で判定できるよう wrap すること）。
// cp はこれを見て再読込し `ng stale` を返す。
var ErrStale = errors.New("stale revision")
