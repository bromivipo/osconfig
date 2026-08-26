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

//go:build benchmark && windows

package packages

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func readCPUTicks() (uint64, float64, error) {
	var creationTime, exitTime, kernelTime, userTime windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creationTime, &exitTime, &kernelTime, &userTime); err != nil {
		return 0, 0, fmt.Errorf("GetProcessTimes: %w", err)
	}
	k := uint64(kernelTime.HighDateTime)<<32 + uint64(kernelTime.LowDateTime)
	u := uint64(userTime.HighDateTime)<<32 + uint64(userTime.LowDateTime)
	// 10,000,000 Filetime units (100ns) per second on Windows
	return k + u, 10000000.0, nil
}
