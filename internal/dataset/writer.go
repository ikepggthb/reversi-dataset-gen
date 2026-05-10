package dataset

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type phaseWriters struct {
	files  map[string]*os.File
	bufs   map[string]*bufio.Writer
	hashes *os.File
	counts map[string]int64
}

func openPhaseWriters(dir string, resume bool) (*phaseWriters, map[string]bool, map[string]int64, error) {
	pw := &phaseWriters{files: map[string]*os.File{}, bufs: map[string]*bufio.Writer{}, counts: map[string]int64{}}
	flags := os.O_CREATE | os.O_WRONLY
	if resume {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	for _, split := range []string{"train", "valid", "test"} {
		path := filepath.Join(dir, split+".rd")
		count, err := recordCount(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, nil, err
		}
		f, err := os.OpenFile(path, flags, 0o644)
		if err != nil {
			return nil, nil, nil, err
		}
		pw.files[split] = f
		pw.bufs[split] = bufio.NewWriter(f)
		pw.counts[split] = count
		needsHeader := !resume
		if resume {
			if st, statErr := os.Stat(path); os.IsNotExist(statErr) || st.Size() == 0 {
				needsHeader = true
			}
		}
		if needsHeader {
			if _, err := pw.bufs[split].WriteString(MagicBitboard); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	hashPath := filepath.Join(dir, "hashes.jsonl")
	hashes := map[string]bool{}
	if resume {
		hashLines, err := readHashes(hashPath, hashes)
		if err != nil && !os.IsNotExist(err) {
			return nil, nil, nil, err
		}
		recordTotal := pw.counts["train"] + pw.counts["valid"] + pw.counts["test"]
		if err == nil && int64(hashLines) != recordTotal {
			return nil, nil, nil, fmt.Errorf("%s: hash sidecar line count %d does not match rd record count %d; use --force to rebuild", hashPath, hashLines, recordTotal)
		}
		if err == nil && len(hashes) != hashLines {
			return nil, nil, nil, fmt.Errorf("%s: duplicate hashes found in sidecar; use --force to rebuild", hashPath)
		}
		if os.IsNotExist(err) && recordTotal > 0 {
			return nil, nil, nil, fmt.Errorf("%s: missing hash sidecar for %d existing records; use --force to rebuild", hashPath, recordTotal)
		}
	}
	hf, err := os.OpenFile(hashPath, flags, 0o644)
	if err != nil {
		return nil, nil, nil, err
	}
	pw.hashes = hf
	return pw, hashes, pw.counts, nil
}

func (w *phaseWriters) write(split string, own, opp uint64, value int16, hash string) error {
	bw := w.bufs[split]
	var rec [RecordSize]byte
	binary.LittleEndian.PutUint64(rec[0:8], own)
	binary.LittleEndian.PutUint64(rec[8:16], opp)
	binary.LittleEndian.PutUint16(rec[16:18], uint16(value))
	if _, err := bw.Write(rec[:]); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w.hashes, hash); err != nil {
		return err
	}
	w.counts[split]++
	return nil
}

func (w *phaseWriters) flush() error {
	for _, bw := range w.bufs {
		if err := bw.Flush(); err != nil {
			return err
		}
	}
	if w.hashes != nil {
		return w.hashes.Sync()
	}
	return nil
}

func (w *phaseWriters) close() {
	for _, f := range w.files {
		_ = f.Close()
	}
	if w.hashes != nil {
		_ = w.hashes.Close()
	}
}

func phaseOutputExists(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "train.rd", "valid.rd", "test.rd", "metadata.json", "stats.json", "hashes.jsonl":
			return true, nil
		}
	}
	return false, nil
}

func recordCount(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if st.Size() == 0 {
		return 0, os.ErrNotExist
	}
	if st.Size() < int64(len(MagicBitboard)) {
		return 0, fmt.Errorf("%s: corrupt rd (size %d, smaller than header); use --resume --repair to rebuild this phase", path, st.Size())
	}
	if (st.Size()-int64(len(MagicBitboard)))%RecordSize != 0 {
		return 0, fmt.Errorf("%s: invalid rd size", path)
	}
	header := make([]byte, len(MagicBitboard))
	if _, err := io.ReadFull(f, header); err != nil {
		return 0, err
	}
	if string(header) != MagicBitboard {
		return 0, fmt.Errorf("%s: invalid rd header", path)
	}
	return (st.Size() - int64(len(MagicBitboard))) / RecordSize, nil
}

func readHashes(path string, out map[string]bool) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	count := 0
	for sc.Scan() {
		h := strings.TrimSpace(sc.Text())
		if h != "" {
			count++
			out[h] = true
		}
	}
	return count, sc.Err()
}
