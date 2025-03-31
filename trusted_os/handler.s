// Copyright 2022 The Armored Witness OS authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include "go_asm.h"
#include "textflag.h"

// Supports tamago >= 1.24 applet runtime.
TEXT ·wakeHandlerGo124(SB),$0-8
	MOVW	handlerG+0(FP), R0
	MOVW	handlerP+4(FP), R1

	CMP	$0, R0
	B.EQ	done

	CMP	$0, R1
	B.EQ	done

	// Call the current runtime's version of WakeG
	B	runtime·WakeG(SB)
done:
	RET

// These values are only used to aid applet runtime backward compatibility
// within wakeHandlerGo123, and represent timer struct offsets in the Go runtime
// for Go version 1.23.x
//
// To determine these values, the following rough process can be used:
// 1. clone https://github.com/usbarmory/tamago-go@tamago-1.23.1
// 2. cd tamago-go/src
// 3. run ./all.bash to build the binaries
// 4. within the cloned repo, create a file src/runtime/test.go with the following contents:
//    package runtime
//
//    import "unsafe"
//
//    type Info struct {
//            GTimer uintptr
//    }
//
//    func GetInfo() Info {
//            gp, _ := GetG()
//            myG := (*g)(unsafe.Pointer(uintptr(gp)))
//            r := Info{}
//            r.GTimer = unsafe.Offsetof(myG.timer)
//            return r
//    }
// 5. within the cloned repo, create a file test.go at the root with the following contents:
//    package main
//
//    func main() {
//            i := runtime.GetInfo()
//            fmt.Printf("%#v", i)
//    }
// 6. From the root of the repo, run: GOOS=tamago GOARCH=arm ./bin/go run --work ./test.go
//    This will fail with some "undefined: ..." errors, but importantly will print out a line line this:
//    WORK=/tmp/go-build3974774761
// 7. Find #define values of interest in /tmp/go-build3974774761/b002/go_asm.h

#define go123_g_timer               212
#define go123_timer_when             12
#define go123_timerWhen__size        12
#define go123_timerWhen_timer         0
#define go123_timerWhen_when          4
#define go123_timer_astate            4
#define go123_timer_ts               44
#define go123_timers_minWhenModified 40
#define go123_timers_heap             4
#define go123_const_timerModified     2

// Supports tamago version >= 1.23.0 && < 1.24.
TEXT ·wakeHandlerGo123(SB),$0-8
	MOVW	handlerG+0(FP), R0
	MOVW	handlerP+4(FP), R1

	CMP	$0, R0
	B.EQ	done

	CMP	$0, R1
	B.EQ	done

	// Code below taken from tamago-go@1.23.1/src/runtime/sys_tamago_arm.s
	MOVW	(go123_g_timer)(R0), R3
	CMP	$0, R3
	B.EQ	done

	// g->timer.when = 1
	MOVW	$1, R1
	MOVW	R1, (go123_timer_when+0)(R3)
	MOVW	$0, R1
	MOVW	R1, (go123_timer_when+4)(R3)

	// g->timer.astate &= timerModified
	// g->timer.state  &= timerModified
	MOVW	(go123_timer_astate)(R3), R2
	ORR	$go123_const_timerModified<<8|go123_const_timerModified, R2, R2
	MOVW	R2, (go123_timer_astate)(R3)

	MOVW	(go123_timer_ts)(R3), R0
	CMP	$0, R0
	B.EQ	done

	// g->timer.ts.minWhenModified = 1
	MOVW	$1, R1
	MOVW	R1, (go123_timers_minWhenModified+0)(R0)
	MOVW	$0, R1
	MOVW	R1, (go123_timers_minWhenModified+4)(R0)

	// len(g->timer.ts.heap)
	MOVW	(go123_timers_heap+4)(R0), R2
	CMP	$0, R2
	B.EQ	done

	// offset to last element
	SUB	$1, R2, R2
	MOVW	$(go123_timerWhen__size), R1
	MUL	R1, R2, R2

	MOVW	(go123_timers_heap)(R0), R0
	CMP	$0, R0
	B.EQ	done

	// g->timer.ts.heap[len-1]
	ADD	R2, R0, R0
	B	check

prev:
	SUB	$(go123_timerWhen__size), R0
	CMP	$0, R0
	B.EQ	done

check:
	// find longest timer as *timers.adjust() might be pending
	MOVW	(go123_timerWhen_when+0)(R0), R1
	CMP	$0xffffffff, R1 // LS word of math.MaxInt64
	B.NE	prev

	MOVW	(go123_timerWhen_when+4)(R0), R1
	CMP	$0x7fffffff, R1 // MS word of math.MaxInt64
	B.NE	prev

	// g->timer.ts.heap[off] = 1
	MOVW	$1, R1
	MOVW	R1, (go123_timerWhen_when+0)(R0)
	MOVW	$0, R1
	MOVW	R1, (go123_timerWhen_when+4)(R0)

done:
	RET


// These defines are only used to aid applet runtime backward compatiblity,
// within wakeHandlerPreGo123, and represent timer structs offsets fo Go <
// 1.23.
#define go122_g_timer				208
#define go122_timer_nextwhen			36
#define go122_timer_status			44
#define go122_const_timerModifiedEarlier	7
#define go122_p_timerModifiedEarliest		2384


// Supports tamago < 1.23 applet runtime.
TEXT ·wakeHandlerPreGo123(SB),$0-8
	MOVW	handlerG+0(FP), R0
	MOVW	handlerP+4(FP), R1

	CMP	$0, R0
	B.EQ	done

	CMP	$0, R1
	B.EQ	done

	MOVW	(go122_g_timer)(R0), R0
	CMP	$0, R0
	B.EQ	done

	// g->timer.nextwhen = 1
	MOVW	$1, R2
	MOVW	R2, (go122_timer_nextwhen+0)(R0)
	MOVW	$0, R2
	MOVW	R2, (go122_timer_nextwhen+4)(R0)

	// g->timer.status = timerModifiedEarlier
	MOVW	$go122_const_timerModifiedEarlier, R2
	MOVW	R2, (go122_timer_status+0)(R0)

	// g->m->p.timerModifiedEarliest = 1
	MOVW	$1, R2
	MOVW	R2, (go122_p_timerModifiedEarliest)(R1)
	MOVW	$0, R2
	MOVW	R2, (go122_p_timerModifiedEarliest+4)(R1)
done:
	RET
