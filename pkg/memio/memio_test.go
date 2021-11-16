// Copyright 2012-2019 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package memio

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"testing"
)

// TestIO tests a set of UintN againt the IO operations
func TestIO(t *testing.T) {
	for _, tt := range tests {
		t.Run(fmt.Sprintf(tt.name), func(t *testing.T) {
			tmpFile, err := ioutil.TempFile("", "io_test")
			if err != nil {
				t.Error(err)
			}
			_, err = tmpFile.Write(make([]byte, 10000))
			if err != nil {
				t.Error(err)
			}
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())
			memPath = tmpFile.Name()
			defer func() { memPath = "/dev/mem" }()

			// Write to the file.
			if err := Write(tt.addr, tt.writeData); err != nil {
				if err.Error() == tt.err {
					return
				}
				t.Fatal(err)
			}

			// Read back the value.
			if err := Read(tt.addr, tt.readData); err != nil {
				if err.Error() == tt.err {
					return
				}
				t.Error(err)
			}

			want := tt.writeData
			got := tt.readData
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("Write(%#016x, %v) = %v; want %v",
					tt.addr, want, got, want)
			}
		})
	}
}

func TestPathError(t *testing.T) {
	// Test invalid path
	for _, tt := range testsInvalid {
		t.Run(fmt.Sprintf(tt.name), func(t *testing.T) {
			memPath = tt.path
			defer func() { memPath = "/dev/mem" }()

			// Write to the file.
			if err := Write(tt.addr, tt.writeData); err != nil {
				want := os.ErrNotExist
				if !errors.Is(err, want) {
					t.Error(err)
				}
			}

			// Read back the value.
			if err := Read(tt.addr, tt.readData); err != nil {
				want := os.ErrNotExist
				if !errors.Is(err, want) {
					t.Error(err)
				}
			}
		})
	}
}

func TestMmap(t *testing.T) {
	// Test invalid file opening
	for _, tt := range testsInvalid {
		t.Run(fmt.Sprintf(tt.name), func(t *testing.T) {
			tmpFile, err := ioutil.TempFile("", "io_test")
			if err != nil {
				t.Error(err)
			}
			_, err = tmpFile.Write(make([]byte, 10000))
			if err != nil {
				t.Error(err)
			}
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())
			memPath = tmpFile.Name()
			defer func() { memPath = "/dev/mem" }()

			// Set error
			tt.err = "This is a dummy error"
			// Set internal functions to dummy function
			Mmap = func(fd int, offset int64, length int, prot int, flags int) ([]byte, error) {
				return nil, errors.New(tt.err)
			}

			// Write to the file.
			if err := Write(tt.addr, tt.writeData); err != nil {
				if err.Error() != tt.err {
					t.Error(err)
				}

			}

			// Read back the value.
			if err := Read(tt.addr, tt.readData); err != nil {
				if err.Error() == tt.err {
					t.Error(err)
				}
			}
		})
	}
}

// TestUnmap tests the error handling of a malfunctioning syscall.Munmap
func TestUnmap(t *testing.T) {

	for _, tt := range tests {
		t.Run(fmt.Sprintf(tt.name), func(t *testing.T) {
			tmpFile, err := ioutil.TempFile("", "io_test")
			if err != nil {
				t.Error(err)
			}
			_, err = tmpFile.Write(make([]byte, 10000))
			if err != nil {
				t.Error(err)
			}
			tmpFile.Close()
			defer os.Remove(tmpFile.Name())
			memPath = tmpFile.Name()
			defer func() { memPath = "/dev/mem" }()

			// Set error
			tt.err = "This is a dummy error"
			// Set internal functions to dummy function
			Munmap = func(mem []byte) error {
				t.Log("Test Munmap")
				return errors.New(tt.err)
			}

			// Write to the file.
			if err := Write(tt.addr, tt.writeData); err != nil {
				if err.Error() == tt.err {
					return
				}
				t.Error(err)
			}

			// Read back the value.
			if err := Read(tt.addr, tt.readData); err != nil {
				if err.Error() == tt.err {
					return
				}
				t.Error(err)
			}

		})
	}
}
func ExampleRead() {
	var data Uint32
	if err := Read(0x1000000, &data); err != nil {
		log.Print(err)
	}
	log.Println(data)
}

func ExampleWrite() {
	data := Uint32(42)
	if err := Write(0x1000000, &data); err != nil {
		log.Print(err)
	}
}
