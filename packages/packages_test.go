//  Copyright 2017 Google Inc. All Rights Reserved.
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

package packages

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	utilmocks "github.com/GoogleCloudPlatform/osconfig/util/mocks"
	"github.com/GoogleCloudPlatform/osconfig/util/utiltest"
	"github.com/golang/mock/gomock"
)

var pkgs = []string{"pkg1", "pkg2"}
var testCtx = context.Background()

type expectedCommand struct {
	cmd    *exec.Cmd
	envs   []string
	stdout []byte
	stderr []byte
	err    error
}

func setExpectations(mockCommandRunner *utilmocks.MockCommandRunner, expectedCommandsChain []expectedCommand) {
	if len(expectedCommandsChain) == 0 {
		return
	}

	var prev *gomock.Call
	for _, expectedCmd := range expectedCommandsChain {
		cmd := expectedCmd.cmd
		if len(expectedCmd.envs) > 0 {
			cmd.Env = append(os.Environ(), expectedCmd.envs...)
		}

		if prev == nil {
			prev = mockCommandRunner.EXPECT().
				Run(gomock.Any(), utilmocks.EqCmd(cmd)).
				Return(expectedCmd.stdout, expectedCmd.stderr, expectedCmd.err).Times(1)
		} else {
			prev = mockCommandRunner.EXPECT().
				Run(gomock.Any(), utilmocks.EqCmd(cmd)).
				After(prev).
				Return(expectedCmd.stdout, expectedCmd.stderr, expectedCmd.err).Times(1)
		}
	}
}

func formatError(err error) string {
	if err == nil {
		return "<nil>"
	}

	return err.Error()
}

func getMockRun(content []byte, err error) func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
	return func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		return content, err
	}
}

// TODO: move this to a common helper package
func helperLoadBytes(name string) ([]byte, error) {
	path := filepath.Join("testdata", name) // relative path
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func TestPackagesMerge(t *testing.T) {
	tests := []struct {
		name string
		p1   Packages
		p2   Packages
		want Packages
	}{
		{
			name: "both empty, expect empty",
			p1:   Packages{},
			p2:   Packages{},
			want: Packages{},
		},
		{
			name: "p1 has scalibr packages, p2 has legacy windows packages, expect merged packages",
			p1: Packages{
				Chocolatey: []*PkgInfo{{Name: "git", Version: "2.40.1", Type: "chocolatey"}},
				WinGet:     []*PkgInfo{{Name: "PowerToys", Version: "0.70.1", Type: "winget"}},
			},
			p2: Packages{
				GooGet: []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet"}},
				WUA:    []*WUAPackage{{Title: "KB123456", UpdateID: "id-1"}},
				QFE:    []*QFEPackage{{Caption: "KB654321", HotFixID: "id-2"}},
				WindowsApplication: []*WindowsApplication{
					{DisplayName: "App1", DisplayVersion: "1.0"},
				},
			},
			want: Packages{
				GooGet:             []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet"}},
				WUA:                []*WUAPackage{{Title: "KB123456", UpdateID: "id-1"}},
				QFE:                []*QFEPackage{{Caption: "KB654321", HotFixID: "id-2"}},
				WindowsApplication: []*WindowsApplication{{DisplayName: "App1", DisplayVersion: "1.0"}},
				Chocolatey:         []*PkgInfo{{Name: "git", Version: "2.40.1", Type: "chocolatey"}},
				WinGet:             []*PkgInfo{{Name: "PowerToys", Version: "0.70.1", Type: "winget"}},
			},
		},
		{
			name: "both have elements in same fields, expect slices concatenated",
			p1: Packages{
				Pip: []*PkgInfo{{Name: "requests", Version: "2.28.0", Type: "pypi"}},
				Gem: []*PkgInfo{{Name: "rake", Version: "13.0.0", Type: "gem"}},
			},
			p2: Packages{
				Pip: []*PkgInfo{{Name: "numpy", Version: "1.24.0", Type: "pypi"}},
				Gem: []*PkgInfo{{Name: "rspec", Version: "3.12.0", Type: "gem"}},
			},
			want: Packages{
				Pip: []*PkgInfo{
					{Name: "requests", Version: "2.28.0", Type: "pypi"},
					{Name: "numpy", Version: "1.24.0", Type: "pypi"},
				},
				Gem: []*PkgInfo{
					{Name: "rake", Version: "13.0.0", Type: "gem"},
					{Name: "rspec", Version: "3.12.0", Type: "gem"},
				},
			},
		},
		{
			name: "duplicate packages across sources, expect deduplicated merged result",
			p1: Packages{
				GooGet: []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet", Purl: "pkg:googet/googet-pkg@1.0.0"}},
				WUA:    []*WUAPackage{{Title: "KB123456", UpdateID: "id-1", Purl: "pkg:generic/KB123456@id-1"}},
				QFE:    []*QFEPackage{{Caption: "KB654321", HotFixID: "id-2", Purl: "pkg:generic/KB654321@id-2"}},
				WindowsApplication: []*WindowsApplication{
					{DisplayName: "App1", DisplayVersion: "1.0", Publisher: "Google", Purl: "pkg:generic/App1@1.0"},
				},
				ZypperPatches: []*ZypperPatch{{Name: "patch-1", Category: "security", Purl: "pkg:generic/patch-1"}},
			},
			p2: Packages{
				GooGet: []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet", Purl: "pkg:googet/googet-pkg@1.0.0"}},
				WUA:    []*WUAPackage{{Title: "KB123456", UpdateID: "id-1", Purl: "pkg:generic/KB123456@id-1"}},
				QFE:    []*QFEPackage{{Caption: "KB654321", HotFixID: "id-2", Purl: "pkg:generic/KB654321@id-2"}},
				WindowsApplication: []*WindowsApplication{
					{DisplayName: "App1", DisplayVersion: "1.0", Publisher: "Google", Purl: "pkg:generic/App1@1.0"},
				},
				ZypperPatches: []*ZypperPatch{{Name: "patch-1", Category: "security", Purl: "pkg:generic/patch-1"}},
			},
			want: Packages{
				GooGet:        []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet", Purl: "pkg:googet/googet-pkg@1.0.0"}},
				ZypperPatches: []*ZypperPatch{{Name: "patch-1", Category: "security", Purl: "pkg:generic/patch-1"}},
				WUA:           []*WUAPackage{{Title: "KB123456", UpdateID: "id-1", Purl: "pkg:generic/KB123456@id-1"}},
				QFE:           []*QFEPackage{{Caption: "KB654321", HotFixID: "id-2", Purl: "pkg:generic/KB654321@id-2"}},
				WindowsApplication: []*WindowsApplication{
					{DisplayName: "App1", DisplayVersion: "1.0", Publisher: "Google", Purl: "pkg:generic/App1@1.0"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p1.Merge(tt.p2)
			utiltest.AssertEquals(t, tt.want, got)
		})
	}
}

func TestMergedInstalledPackagesProvider(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockScalibr := newMockInstalledPackagesProvider(ctrl)
	mockDefault := newMockInstalledPackagesProvider(ctrl)

	provider := mergedInstalledPackagesProvider{
		scalibrProvider: mockScalibr,
		defaultProvider: mockDefault,
	}

	tests := []struct {
		name        string
		scalibrPkgs Packages
		scalibrErr  error
		defaultPkgs Packages
		defaultErr  error
		wantPkgs    Packages
		wantErr     error
	}{
		{
			name: "both providers succeed, returns merged packages and nil error",
			scalibrPkgs: Packages{
				Chocolatey: []*PkgInfo{{Name: "git", Version: "2.40.1", Type: "chocolatey"}},
				WinGet:     []*PkgInfo{{Name: "PowerToys", Version: "0.70.1", Type: "winget"}},
			},
			defaultPkgs: Packages{
				GooGet: []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet"}},
				WUA:    []*WUAPackage{{Title: "KB123456", UpdateID: "id-1"}},
			},
			wantPkgs: Packages{
				GooGet:     []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet"}},
				WUA:        []*WUAPackage{{Title: "KB123456", UpdateID: "id-1"}},
				Chocolatey: []*PkgInfo{{Name: "git", Version: "2.40.1", Type: "chocolatey"}},
				WinGet:     []*PkgInfo{{Name: "PowerToys", Version: "0.70.1", Type: "winget"}},
			},
			wantErr: nil,
		},
		{
			name:       "scalibr provider fails, returns default packages and error",
			scalibrErr: errors.New("scalibr scan failed"),
			defaultPkgs: Packages{
				GooGet: []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet"}},
			},
			wantPkgs: Packages{
				GooGet: []*PkgInfo{{Name: "googet-pkg", Version: "1.0.0", Type: "googet"}},
			},
			wantErr: errors.New("scalibr error: scalibr scan failed"),
		},
		{
			name: "default provider fails, returns scalibr packages and error",
			scalibrPkgs: Packages{
				Chocolatey: []*PkgInfo{{Name: "git", Version: "2.40.1", Type: "chocolatey"}},
			},
			defaultErr: errors.New("wua query failed"),
			wantPkgs: Packages{
				Chocolatey: []*PkgInfo{{Name: "git", Version: "2.40.1", Type: "chocolatey"}},
			},
			wantErr: errors.New("default error: wua query failed"),
		},
		{
			name:       "both providers fail, returns aggregated error",
			scalibrErr: errors.New("scalibr scan failed"),
			defaultErr: errors.New("wua query failed"),
			wantPkgs:   Packages{},
			wantErr:    errors.New("scalibr error: scalibr scan failed\ndefault error: wua query failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockScalibr.EXPECT().GetInstalledPackages(ctx).Return(tt.scalibrPkgs, tt.scalibrErr)
			mockDefault.EXPECT().GetInstalledPackages(ctx).Return(tt.defaultPkgs, tt.defaultErr)

			gotPkgs, gotErr := provider.GetInstalledPackages(ctx)
			utiltest.AssertErrorMatch(t, gotErr, tt.wantErr)
			utiltest.AssertEquals(t, tt.wantPkgs, gotPkgs)
		})
	}
}
