// Copyright (C) 2014 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

// +build integration

package integration

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/syncthing/protocol"
	"github.com/syncthing/syncthing/internal/config"
)

func TestSyncClusterWithoutVersioning(t *testing.T) {
	// Use no versioning
	id, _ := protocol.DeviceIDFromString(id2)
	cfg, _ := config.Load("h2/config.xml", id)
	fld := cfg.Folders()["default"]
	fld.Versioning = config.VersioningConfiguration{}
	cfg.SetFolder(fld)
	cfg.Save()

	testSyncCluster(t)
}

func TestSyncClusterSimpleVersioning(t *testing.T) {
	// Use simple versioning
	id, _ := protocol.DeviceIDFromString(id2)
	cfg, _ := config.Load("h2/config.xml", id)
	fld := cfg.Folders()["default"]
	fld.Versioning = config.VersioningConfiguration{
		Type:   "simple",
		Params: map[string]string{"keep": "5"},
	}
	cfg.SetFolder(fld)
	cfg.Save()

	testSyncCluster(t)
}

func TestSyncClusterStaggeredVersioning(t *testing.T) {
	// Use staggered versioning
	id, _ := protocol.DeviceIDFromString(id2)
	cfg, _ := config.Load("h2/config.xml", id)
	fld := cfg.Folders()["default"]
	fld.Versioning = config.VersioningConfiguration{
		Type: "staggered",
	}
	cfg.SetFolder(fld)
	cfg.Save()

	testSyncCluster(t)
}

func testSyncCluster(t *testing.T) {
	// This tests syncing files back and forth between three cluster members.
	// Their configs are in h1, h2 and h3. The folder "default" is shared
	// between all and stored in s1, s2 and s3 respectively.
	//
	// Another folder is shared between 1 and 2 only, in s12-1 and s12-2. A
	// third folders is shared between 2 and 3, in s23-2 and s23-3.

	const (
		numFiles    = 100
		fileSizeExp = 20
		iterations  = 3
	)
	log.Printf("Testing with numFiles=%d, fileSizeExp=%d, iterations=%d", numFiles, fileSizeExp, iterations)

	log.Println("Cleaning...")
	err := removeAll("s1", "s12-1",
		"s2", "s12-2", "s23-2",
		"s3", "s23-3",
		"h1/index*", "h2/index*", "h3/index*")
	if err != nil {
		t.Fatal(err)
	}

	// Create initial folder contents. All three devices have stuff in
	// "default", which should be merged. The other two folders are initially
	// empty on one side.

	log.Println("Generating files...")

	err = generateFiles("s1", numFiles, fileSizeExp, "../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	err = generateFiles("s12-1", numFiles, fileSizeExp, "../LICENSE")
	if err != nil {
		t.Fatal(err)
	}

	// We'll use this file for appending data without modifying the time stamp.
	fd, err := os.Create("s1/test-appendfile")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fd.WriteString("hello\n")
	if err != nil {
		t.Fatal(err)
	}
	err = fd.Close()
	if err != nil {
		t.Fatal(err)
	}

	err = generateFiles("s2", numFiles, fileSizeExp, "../LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	err = generateFiles("s23-2", numFiles, fileSizeExp, "../LICENSE")
	if err != nil {
		t.Fatal(err)
	}

	err = generateFiles("s3", numFiles, fileSizeExp, "../LICENSE")
	if err != nil {
		t.Fatal(err)
	}

	// Prepare the expected state of folders after the sync
	c1, err := directoryContents("s1")
	if err != nil {
		t.Fatal(err)
	}
	c2, err := directoryContents("s2")
	if err != nil {
		t.Fatal(err)
	}
	c3, err := directoryContents("s3")
	if err != nil {
		t.Fatal(err)
	}
	e1 := mergeDirectoryContents(c1, c2, c3)
	e2, err := directoryContents("s12-1")
	if err != nil {
		t.Fatal(err)
	}
	e3, err := directoryContents("s23-2")
	if err != nil {
		t.Fatal(err)
	}
	expected := [][]fileInfo{e1, e2, e3}

	// Start the syncers
	p, err := scStartProcesses()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range p {
			p[i].stop()
		}
	}()

	log.Println("Waiting for startup...")
	for _, dev := range p {
		waitForScan(dev)
	}

	for count := 0; count < iterations; count++ {
		log.Println("Forcing rescan...")

		// Force rescan of folders
		for i, device := range p {
			if err := device.rescan("default"); err != nil {
				t.Fatal(err)
			}
			if i < 2 {
				if err := device.rescan("s12"); err != nil {
					t.Fatal(err)
				}
			}
			if i > 1 {
				if err := device.rescan("s23"); err != nil {
					t.Fatal(err)
				}
			}
		}

		// Sync stuff and verify it looks right
		err = scSyncAndCompare(p, expected)
		if err != nil {
			t.Error(err)
			break
		}

		log.Println("Altering...")

		// Alter the source files for another round
		err = alterFiles("s1")
		if err != nil {
			t.Error(err)
			break
		}
		err = alterFiles("s12-1")
		if err != nil {
			t.Error(err)
			break
		}
		err = alterFiles("s23-2")
		if err != nil {
			t.Error(err)
			break
		}

		// Alter the "test-appendfile" without changing it's modification time. Sneaky!
		fi, err := os.Stat("s1/test-appendfile")
		if err != nil {
			t.Fatal(err)
		}
		fd, err := os.OpenFile("s1/test-appendfile", os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = fd.Seek(0, os.SEEK_END)
		if err != nil {
			t.Fatal(err)
		}
		_, err = fd.WriteString("more data\n")
		if err != nil {
			t.Fatal(err)
		}
		err = fd.Close()
		if err != nil {
			t.Fatal(err)
		}
		err = os.Chtimes("s1/test-appendfile", fi.ModTime(), fi.ModTime())
		if err != nil {
			t.Fatal(err)
		}

		// Prepare the expected state of folders after the sync
		e1, err = directoryContents("s1")
		if err != nil {
			t.Fatal(err)
		}
		e2, err = directoryContents("s12-1")
		if err != nil {
			t.Fatal(err)
		}
		e3, err = directoryContents("s23-2")
		if err != nil {
			t.Fatal(err)
		}
		expected = [][]fileInfo{e1, e2, e3}
	}
}

func scStartProcesses() ([]syncthingProcess, error) {
	p := make([]syncthingProcess, 3)

	p[0] = syncthingProcess{ // id1
		instance: "1",
		argv:     []string{"-home", "h1"},
		port:     8081,
		apiKey:   apiKey,
	}
	err := p[0].start()
	if err != nil {
		return nil, err
	}

	p[1] = syncthingProcess{ // id2
		instance: "2",
		argv:     []string{"-home", "h2"},
		port:     8082,
		apiKey:   apiKey,
	}
	err = p[1].start()
	if err != nil {
		p[0].stop()
		return nil, err
	}

	p[2] = syncthingProcess{ // id3
		instance: "3",
		argv:     []string{"-home", "h3"},
		port:     8083,
		apiKey:   apiKey,
	}
	err = p[2].start()
	if err != nil {
		p[0].stop()
		p[1].stop()
		return nil, err
	}

	return p, nil
}

func scSyncAndCompare(p []syncthingProcess, expected [][]fileInfo) error {
	log.Println("Syncing...")

	// Special handling because we know which devices share which folders...
	if err := awaitCompletion("default", p...); err != nil {
		return err
	}
	if err := awaitCompletion("s12", p[0], p[1]); err != nil {
		return err
	}
	if err := awaitCompletion("s23", p[1], p[2]); err != nil {
		return err
	}

	// This is necessary, or all files won't be in place even when everything
	// is already reported in sync. Why?!
	time.Sleep(5 * time.Second)

	log.Println("Checking...")

	for _, dir := range []string{"s1", "s2", "s3"} {
		actual, err := directoryContents(dir)
		if err != nil {
			return err
		}
		if err := compareDirectoryContents(actual, expected[0]); err != nil {
			return fmt.Errorf("%s: %v", dir, err)
		}
	}

	for _, dir := range []string{"s12-1", "s12-2"} {
		actual, err := directoryContents(dir)
		if err != nil {
			return err
		}
		if err := compareDirectoryContents(actual, expected[1]); err != nil {
			return fmt.Errorf("%s: %v", dir, err)
		}
	}

	for _, dir := range []string{"s23-2", "s23-3"} {
		actual, err := directoryContents(dir)
		if err != nil {
			return err
		}
		if err := compareDirectoryContents(actual, expected[2]); err != nil {
			return fmt.Errorf("%s: %v", dir, err)
		}
	}

	return nil
}
