// https://github.com/usbarmory/tamago
//
// Copyright (c) WithSecure Corporation
// https://foundry.withsecure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "go_asm.h"
#include "textflag.h"

TEXT ·wakeAppletHandler(SB),$0-8
	MOVW	appletHandlerG+0(FP), R0
	MOVW	appletHandlerP+4(FP), R1

	CMP	$0, R0
	B.EQ	done

	CMP	$0, R1
	B.EQ	done

	B	runtime·WakeG(SB)
done:
	RET
