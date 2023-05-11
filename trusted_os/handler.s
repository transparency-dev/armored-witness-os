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
