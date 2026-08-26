//  Copyright 2026 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

//go:build benchmark && linux

package packages

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func readCPUTicks() (uint64, float64, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/self/stat: %w", err)
	}
	statStr := string(data)
	lastParen := strings.LastIndex(statStr, ")")
	if lastParen == -1 || lastParen+2 >= len(statStr) {
		return 0, 0, fmt.Errorf("invalid stat format")
	}
	fields := strings.Fields(statStr[lastParen+2:])
	if len(fields) < 13 {
		return 0, 0, fmt.Errorf("invalid stat fields count: %d", len(fields))
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil {
		return 0, 0, fmt.Errorf("error parsing utime: %w", err1)
	}
	if err2 != nil {
		return 0, 0, fmt.Errorf("error parsing stime: %w", err2)
	}
	// 100.0 is standard SC_CLK_TCK ticks per second on Linux
	return utime + stime, 100.0, nil
}
