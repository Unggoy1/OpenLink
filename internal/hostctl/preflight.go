package hostctl

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type Report struct {
	SchemaVersion  int           `json:"schema_version"`
	SHA256         string        `json:"sha256"`
	SupportedBuild bool          `json:"supported_build"`
	ControlReady   bool          `json:"control_ready"`
	Reason         string        `json:"reason"`
	Targets        []TargetCheck `json:"targets"`
	PendingGates   []string      `json:"pending_gates"`
}

type TargetCheck struct {
	Name     string `json:"name"`
	RVA      uint64 `json:"rva"`
	Verified bool   `json:"verified"`
	Reason   string `json:"reason"`
}

// InspectFile reads only the selected file. A successful report identifies an
// on-disk input; it does not authorize or establish any runtime operation.
func InspectFile(path string) (Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Report{}, err
	}
	if !st.Mode().IsRegular() {
		return Report{}, errors.New("input must be a regular file")
	}
	return inspect(f, st.Size(), b002Manifest())
}

func inspect(reader io.ReaderAt, size int64, manifest Manifest) (Report, error) {
	report := Report{SchemaVersion: 1, Targets: []TargetCheck{},
		PendingGates: []string{"variant_consumer", "engine_dispatch", "server_lifecycle"}}
	// Validate bounded headers/ranges first. debug/pe is not a hardened parser;
	// do not let arbitrary unknown builds reach its symbol/relocation parser.
	if err := validateLayout(reader, size); err != nil {
		return report, err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.NewSectionReader(reader, 0, size))
	if err != nil {
		return report, fmt.Errorf("hash input: %w", err)
	}
	if n != size {
		return report, io.ErrUnexpectedEOF
	}
	digest := h.Sum(nil)
	report.SHA256 = hex.EncodeToString(digest)
	if !bytes.Equal(digest, manifest.SHA256[:]) {
		report.Reason = "unknown_build"
		return report, nil
	}
	file, err := pe.NewFile(io.NewSectionReader(reader, 0, size))
	if err != nil {
		return report, fmt.Errorf("parse known PE: %w", err)
	}
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || file.Machine != manifest.Machine || header.ImageBase != manifest.ImageBase {
		report.Reason = "unsupported_pe"
		return report, nil
	}
	report.SupportedBuild = true
	report.Reason = "verified_file_only"
	for _, target := range manifest.Targets {
		check, err := checkTarget(reader, file.Sections, target)
		if err != nil {
			return report, err
		}
		report.Targets = append(report.Targets, check)
		if !check.Verified {
			report.SupportedBuild = false
			report.Reason = "target_validation_failed"
		}
	}
	// ControlReady stays false until independent runtime gates are implemented.
	return report, nil
}

func checkTarget(reader io.ReaderAt, sections []*pe.Section, target Target) (TargetCheck, error) {
	check := TargetCheck{Name: target.Name, RVA: target.RVA, Reason: "not_file_backed"}
	for _, section := range sections {
		start := uint64(section.VirtualAddress)
		span := max(uint64(section.VirtualSize), uint64(section.Size))
		if target.RVA < start || target.RVA-start >= span {
			continue
		}
		delta := target.RVA - start
		if delta > uint64(section.Size) || uint64(len(target.Prefix)) > uint64(section.Size)-delta {
			return check, nil
		}
		if section.Characteristics&pe.IMAGE_SCN_MEM_EXECUTE == 0 {
			check.Reason = "not_executable"
			return check, nil
		}
		var prefix [16]byte
		if _, err := reader.ReadAt(prefix[:], int64(uint64(section.Offset)+delta)); err != nil {
			return check, fmt.Errorf("read target %s: %w", target.Name, err)
		}
		if prefix != target.Prefix {
			check.Reason = "prefix_mismatch"
			return check, nil
		}
		check.Verified, check.Reason = true, "verified"
		return check, nil
	}
	return check, nil
}

type addressRange struct{ start, end uint64 }

func validateLayout(reader io.ReaderAt, size int64) error {
	if size < 0 {
		return errors.New("negative file size")
	}
	read := func(offset uint64, length int) ([]byte, error) {
		if offset > uint64(size) || uint64(length) > uint64(size)-offset {
			return nil, io.ErrUnexpectedEOF
		}
		b := make([]byte, length)
		_, err := reader.ReadAt(b, int64(offset))
		return b, err
	}
	dos, err := read(0, 64)
	if err != nil {
		return fmt.Errorf("DOS header: %w", err)
	}
	if string(dos[:2]) != "MZ" {
		return errors.New("input is not an MZ executable")
	}
	peOffset := uint64(binary.LittleEndian.Uint32(dos[0x3c:]))
	if peOffset < 64 {
		return errors.New("PE header overlaps DOS header")
	}
	coff, err := read(peOffset, 24)
	if err != nil {
		return fmt.Errorf("PE header: %w", err)
	}
	if string(coff[:4]) != "PE\x00\x00" {
		return errors.New("missing PE signature")
	}
	count := uint64(binary.LittleEndian.Uint16(coff[6:]))
	optionalSize := int(binary.LittleEndian.Uint16(coff[20:]))
	if count == 0 || optionalSize < 2 {
		return errors.New("missing executable headers")
	}
	optional, err := read(peOffset+24, optionalSize)
	if err != nil {
		return fmt.Errorf("optional header: %w", err)
	}
	var minimum int
	switch binary.LittleEndian.Uint16(optional) {
	case 0x20b:
		minimum = 112
	case 0x10b:
		minimum = 96
	default:
		return errors.New("invalid optional header magic")
	}
	if optionalSize < minimum {
		return errors.New("truncated optional header")
	}
	directories := uint64(binary.LittleEndian.Uint32(optional[minimum-4:]))
	if directories > uint64(optionalSize-minimum)/8 {
		return errors.New("truncated data directory table")
	}
	table := peOffset + 24 + uint64(optionalSize)
	tableEnd := table + count*40
	if tableEnd > uint64(size) {
		return errors.New("truncated section table")
	}
	headersEnd := uint64(binary.LittleEndian.Uint32(optional[60:]))
	if headersEnd < tableEnd || headersEnd > uint64(size) {
		return errors.New("invalid SizeOfHeaders")
	}
	virtual := make([]addressRange, 0, count)
	raw := make([]addressRange, 0, count)
	for i := uint64(0); i < count; i++ {
		section, err := read(table+i*40, 40)
		if err != nil {
			return err
		}
		vsize := uint64(binary.LittleEndian.Uint32(section[8:]))
		va := uint64(binary.LittleEndian.Uint32(section[12:]))
		rsize := uint64(binary.LittleEndian.Uint32(section[16:]))
		rptr := uint64(binary.LittleEndian.Uint32(section[20:]))
		if span := max(vsize, rsize); span != 0 {
			if va+span > 1<<32 {
				return errors.New("RVA range overflows 32 bits")
			}
			virtual = append(virtual, addressRange{va, va + span})
		}
		if rsize != 0 {
			if rptr < headersEnd || rptr > uint64(size) || rsize > uint64(size)-rptr {
				return errors.New("section raw data outside file or overlaps headers")
			}
			raw = append(raw, addressRange{rptr, rptr + rsize})
		}
	}
	for _, ranges := range [][]addressRange{virtual, raw} {
		sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
		for i := 1; i < len(ranges); i++ {
			if ranges[i].start < ranges[i-1].end {
				return errors.New("overlapping section ranges")
			}
		}
	}
	return nil
}
