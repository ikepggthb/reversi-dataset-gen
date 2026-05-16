package dataset

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ikepggthb/reversi-dataset-gen/internal/board"
)

type phaseWriters struct {
	files  map[string]*os.File
	bufs   map[string]*bufio.Writer
	counts map[string]int64
}

func openPhaseWriters(dir string, resume bool) (*phaseWriters, map[string]bool, map[string]int64, error) {
	pw := &phaseWriters{files: map[string]*os.File{}, bufs: map[string]*bufio.Writer{}, counts: map[string]int64{}}
	success := false
	defer func() {
		if !success {
			pw.close()
		}
	}()
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
	hashes := map[string]bool{}
	if resume {
		for _, split := range []string{"train", "valid", "test"} {
			path := filepath.Join(dir, split+".rd")
			if err := readRecordHashes(path, hashes); err != nil && !os.IsNotExist(err) {
				return nil, nil, nil, err
			}
		}
	}
	success = true
	return pw, hashes, pw.counts, nil
}

func (w *phaseWriters) write(split string, own, opp uint64, value int16) error {
	bw := w.bufs[split]
	var rec [RecordSize]byte
	binary.LittleEndian.PutUint64(rec[0:8], own)
	binary.LittleEndian.PutUint64(rec[8:16], opp)
	binary.LittleEndian.PutUint16(rec[16:18], uint16(value))
	if _, err := bw.Write(rec[:]); err != nil {
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
	return nil
}

func (w *phaseWriters) close() {
	for _, f := range w.files {
		_ = f.Close()
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
		case "train.rd", "valid.rd", "test.rd", "run_state.json", "metadata.json", "stats.json", "hashes.jsonl":
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

func readRecordHashes(path string, out map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return os.ErrNotExist
	}
	if st.Size() < int64(len(MagicBitboard)) {
		return fmt.Errorf("%s: corrupt rd (size %d, smaller than header); use --resume --repair to rebuild this phase", path, st.Size())
	}
	if (st.Size()-int64(len(MagicBitboard)))%RecordSize != 0 {
		return fmt.Errorf("%s: invalid rd size; use --resume --repair to rebuild this phase", path)
	}
	header := make([]byte, len(MagicBitboard))
	if _, err := io.ReadFull(f, header); err != nil {
		return err
	}
	if string(header) != MagicBitboard {
		return fmt.Errorf("%s: invalid rd header; use --resume --repair to rebuild this phase", path)
	}
	var rec [RecordSize]byte
	for {
		_, err := io.ReadFull(f, rec[:])
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		own := binary.LittleEndian.Uint64(rec[0:8])
		opponent := binary.LittleEndian.Uint64(rec[8:16])
		out[board.CanonicalHashBits(own, opponent)] = true
	}
}
