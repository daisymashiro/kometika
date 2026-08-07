package commands

import (
	"testing"

	"mybot/internal/api/terabox"
)

func TestSumFileSizes(t *testing.T) {
	files := []terabox.TeraboxUniversalData{
		{FileName: "a.mp4", FileSize: "1.5 GB"},
		{FileName: "b.mp4", FileSize: "512 MB"},
		{FileName: "c.zip", FileSize: "1 GB"},
		{FileName: "d.bin", FileSize: "250 MB"},
	}
	total := sumFileSizes(files)
	expect := int64(1.5*1024*1024*1024) + int64(512*1024*1024) + int64(1024*1024*1024) + int64(250*1024*1024)
	if total != expect {
		t.Fatalf("sumFileSizes = %d, mau %d", total, expect)
	}

	// Total 3.25 GB > batas 1.9 GB -> harus ditolak untuk ZIP.
	if total <= zipBatchMaxSize {
		t.Fatalf("total %d harusnya melebihi batas %d", total, zipBatchMaxSize)
	}

	// FileSize kosong / tak dikenal = 0, tidak merusak hitungan.
	if sumFileSizes([]terabox.TeraboxUniversalData{{FileName: "x", FileSize: ""}}) != 0 {
		t.Fatal("FileSize kosong harus 0")
	}
}
