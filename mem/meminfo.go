// Copyright 2016 Joel Scoble and The JoeFriday authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package mem gets and processes /proc/meminfo, returning the data in the
// appropriate format.
package mem

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/SermoDigital/helpers"

	fb "github.com/google/flatbuffers/go"
	joe "github.com/mohae/joefriday"
)

type Info struct {
	Timestamp    int64
	MemTotal     int
	MemFree      int
	MemAvailable int
	Buffers      int
	Cached       int
	SwapCached   int
	Active       int
	Inactive     int
	SwapTotal    int
	SwapFree     int
}

// Serialize serializes the Info using flatbuffers.
func (i *Info) Serialize() []byte {
	builder := fb.NewBuilder(0)
	DataStart(builder)
	DataAddTimestamp(builder, int64(i.Timestamp))
	DataAddMemTotal(builder, int64(i.MemTotal))
	DataAddMemFree(builder, int64(i.MemFree))
	DataAddMemAvailable(builder, int64(i.MemAvailable))
	DataAddBuffers(builder, int64(i.Buffers))
	DataAddCached(builder, int64(i.Cached))
	DataAddSwapCached(builder, int64(i.SwapCached))
	DataAddActive(builder, int64(i.Active))
	DataAddInactive(builder, int64(i.Inactive))
	DataAddSwapTotal(builder, int64(i.SwapTotal))
	DataAddSwapFree(builder, int64(i.SwapFree))
	builder.Finish(DataEnd(builder))
	return builder.Bytes[builder.Head():]
}

// Deserialize deserializes bytes representing flatbuffers serialized Data
// into *Info.  If the bytes are not from flatbuffers serialization of
// Data, it is a programmer error and a panic will occur.
func Deserialize(p []byte) *Info {
	data := GetRootAsData(p, 0)
	info := &Info{}
	info.Timestamp = data.Timestamp()
	info.MemTotal = int(data.MemTotal())
	info.MemFree = int(data.MemFree())
	info.MemAvailable = int(data.MemAvailable())
	info.Buffers = int(data.Buffers())
	info.Cached = int(data.Cached())
	info.SwapCached = int(data.SwapCached())
	info.Active = int(data.Active())
	info.Inactive = int(data.Inactive())
	info.SwapTotal = int(data.SwapTotal())
	info.SwapFree = int(data.SwapFree())
	return info
}

func (d *Info) String() string {
	return fmt.Sprintf("Timestamp: %v\nMemTotal:\t%d\tMemFree:\t%d\tMemAvailable:\t%d\tActive:\t%d\tInactive:\t%d\nCached:\t\t%d\tBuffers\t:%d\nSwapTotal:\t%d\tSwapCached:\t%d\tSwapFree:\t%d\n", time.Unix(0, d.Timestamp).UTC(), d.MemTotal, d.MemFree, d.MemAvailable, d.Active, d.Inactive, d.Cached, d.Buffers, d.SwapTotal, d.SwapCached, d.SwapFree)
}

var (
	memTotal     = []byte("MemTotal")
	memFree      = []byte("MemFree")
	memAvailable = []byte("MemAvailable")
	buffers      = []byte("Buffers")
	cached       = []byte("Cached")
	swapCached   = []byte("SwapCached")
	active       = []byte("Active")
	inactive     = []byte("Inactive")
	swapTotal    = []byte("SwapTotal")
	swapFree     = []byte("SwapFree")
)

// GetInfo returns some of the results of /proc/meminfo.
func GetInfo() (*Info, error) {
	buf, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, err
	}

	f := Info{
		Timestamp: time.Now().UTC().UnixNano(),
	}
	for p := 0; ; {
		// Skip to the colon.
		o := bytes.IndexByte(buf[p:], ':')
		if o < 0 {
			break
		}
		i := p + o
		name := buf[p:i]

		for _, c := range buf[i+1:] {
			i++
			if c != ' ' {
				break
			}
		}
		p = i
		for _, c := range buf[i:] {
			if c > '9' || c < '0' {
				break
			}
			i++
		}
		v, err := helpers.ParseUint(buf[p:i])
		if err != nil {
			return nil, err
		}
		// Skip to the end.
		p = i + bytes.IndexByte(buf[i:], '\n') + 1

		switch {
		case bytes.Equal(name, memTotal):
			f.MemTotal = int(v)
		case bytes.Equal(name, memFree):
			f.MemFree = int(v)
		case bytes.Equal(name, memAvailable):
			f.MemAvailable = int(v)
		case bytes.Equal(name, buffers):
			f.Buffers = int(v)
		case bytes.Equal(name, cached):
			f.Cached = int(v)
		case bytes.Equal(name, swapCached):
			f.SwapCached = int(v)
		case bytes.Equal(name, active):
			f.Active = int(v)
		case bytes.Equal(name, inactive):
			f.Inactive = int(v)
		case bytes.Equal(name, swapTotal):
			f.SwapTotal = int(v)
		case bytes.Equal(name, swapFree):
			f.SwapFree = int(v)
		}
	}
	return &f, nil
}

// GetData returns the current meminfo as flatbuffer serialized bytes.
func GetData() ([]byte, error) {
	inf, err := GetInfo()
	if err != nil {
		return nil, err
	}
	return inf.Serialize(), nil
}

// DataTicker gathers the meminfo on a ticker, whose interval is defined by
// the received duration, and sends the results to the channel.  The output
// is Flatbuffers serialized Data.  Any error encountered during processing
// is sent to the error channel.  Processing will continue
//
// Either closing the done channel or sending struct{} to the done channel
// will result in function exit.  The out channel is closed on exit.
//
// This pre-allocates the builder and everything other than the []byte that
// gets sent to the out channel to reduce allocations, as this is expected
// to be both a frequent and a long-running process.  Doing so reduces
// byte allocations per tick just ~ 42%.
func DataTicker(interval time.Duration, outCh chan []byte, done chan struct{}, errCh chan error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(outCh)
	// predeclare some vars
	var l, i, pos int
	var t int64
	var v byte
	var name string
	// premake some temp slices
	val := make([]byte, 0, 32)
	// just reset the bldr at the end of every ticker
	bldr := fb.NewBuilder(0)
	// Some hopes to jump through to ensure we don't get a ErrBufferFull; which was
	// occuring with var buf bufio.Reader (which works in the bench code)
	var bs []byte
	tmp := bytes.NewBuffer(bs)
	buf := bufio.NewReaderSize(tmp, 1536)
	tmp = nil
	// ticker
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// The current timestamp is always in UTC
			t = time.Now().UTC().UnixNano()
			f, err := os.Open("/proc/meminfo")
			if err != nil {
				errCh <- joe.Error{Type: "mem", Op: "open /proc/meminfo", Err: err}
				continue
			}
			buf.Reset(f)
			DataStart(bldr)
			DataAddTimestamp(bldr, t)
			for {
				if l == 16 {
					break
				}
				line, err := buf.ReadSlice('\n')
				if err != nil {
					if err == io.EOF {
						break
					}
					errCh <- joe.Error{Type: "mem", Op: "read command results", Err: err}
					break
				}
				l++
				if l > 8 && l < 15 {
					continue
				}
				// first grab the key name (everything up to the ':')
				for i, v = range line {
					if v == 0x3A {
						pos = i + 1
						break
					}
					val = append(val, v)
				}
				name = string(val[:])
				val = val[:0]
				// skip all spaces
				for i, v = range line[pos:] {
					if v != 0x20 {
						pos += i
						break
					}
				}

				// grab the numbers
				for _, v = range line[pos:] {
					if v == 0x20 || v == '\r' {
						break
					}
					val = append(val, v)
				}
				// any conversion error results in 0
				i, err = strconv.Atoi(string(val[:]))
				if err != nil {
					errCh <- joe.Error{Type: "mem", Op: "convert to int", Err: err}
					continue
				}
				val = val[:0]
				if name == "MemTotal" {
					DataAddMemTotal(bldr, int64(i))
					continue
				}
				if name == "MemFree" {
					DataAddMemFree(bldr, int64(i))
					continue
				}
				if name == "MemAvailable" {
					DataAddMemAvailable(bldr, int64(i))
					continue
				}
				if name == "Buffers" {
					DataAddBuffers(bldr, int64(i))
					continue
				}
				if name == "Cached" {
					DataAddMemAvailable(bldr, int64(i))
					continue
				}
				if name == "SwapCached" {
					DataAddSwapCached(bldr, int64(i))
					continue
				}
				if name == "Active" {
					DataAddActive(bldr, int64(i))
					continue
				}
				if name == "Inactive" {
					DataAddInactive(bldr, int64(i))
					continue
				}
				if name == "SwapTotal" {
					DataAddSwapTotal(bldr, int64(i))
					continue
				}
				if name == "SwapFree" {
					DataAddSwapFree(bldr, int64(i))
					continue
				}
			}
			f.Close()
			bldr.Finish(DataEnd(bldr))
			data := bldr.Bytes[bldr.Head():]
			outCh <- data
			bldr.Reset()
			l = 0
		}
	}
}

func (d *Data) String() string {
	return fmt.Sprintf("Timestamp: %v\nMemTotal:\t%d\tMemFree:\t%d\tMemAvailable:\t%d\tActive:\t%d\tInactive:\t%d\nCached:\t\t%d\tBuffers\t:%d\nSwapTotal:\t%d\tSwapCached:\t%d\tSwapFree:\t%d\n", time.Unix(0, d.Timestamp()).UTC(), d.MemTotal(), d.MemFree(), d.MemAvailable(), d.Active(), d.Inactive(), d.Cached(), d.Buffers(), d.SwapTotal(), d.SwapCached(), d.SwapFree())
}
