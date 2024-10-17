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

#define g_timer				208
#define timer_nextwhen			36
#define timer_status			44
#define const_timerModifiedEarlier	7
#define p_timerModifiedEarliest		2384

TEXT ·wakeHandler(SB),$0-8
	MOVW	handlerG+0(FP), R0
	MOVW	handlerP+4(FP), R1

	CMP	$0, R0
	B.EQ	done

	CMP	$0, R1
	B.EQ	done

	B	runtime·WakeG(SB)
done:
	RET

// In order to support applets built with tamago < 1.23 the previous version of
// WakeG is made available here.
TEXT ·wakeHandlerPreGo123(SB),$0-8
	MOVW	handlerG+0(FP), R0
	MOVW	handlerP+4(FP), R1

	CMP	$0, R0
	B.EQ	done

	CMP	$0, R1
	B.EQ	done

	MOVW	(g_timer)(R0), R0
	CMP	$0, R0
	B.EQ	done

	// g->timer.nextwhen = 1
	MOVW	$1, R2
	MOVW	R2, (timer_nextwhen+0)(R0)
	MOVW	$0, R2
	MOVW	R2, (timer_nextwhen+4)(R0)

	// g->timer.status = timerModifiedEarlier
	MOVW	$const_timerModifiedEarlier, R2
	MOVW	R2, (timer_status+0)(R0)

	// g->m->p.timerModifiedEarliest = 1
	MOVW	$1, R2
	MOVW	R2, (p_timerModifiedEarliest)(R1)
	MOVW	$0, R2
	MOVW	R2, (p_timerModifiedEarliest+4)(R1)
done:
	RET
