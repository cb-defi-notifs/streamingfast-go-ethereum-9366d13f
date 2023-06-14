package firehose

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Printer interface {
	Print(input ...string)
}

type DelegateToWriterPrinter struct {
	writer io.Writer
}

func (p *DelegateToWriterPrinter) Disabled() bool {
	return false
}

func (p *DelegateToWriterPrinter) Print(input ...string) {
	line := "FIRE " + strings.Join(input, " ") + "\n"
	var written int
	var err error
	loops := 10
	for i := 0; i < loops; i++ {
		written, err = fmt.Fprint(p.writer, line)

		if len(line) == written {
			return
		}

		line = line[written:]

		if i == loops-1 {
			break
		}
	}

	errstr := fmt.Sprintf("\nFIREHOSE FAILED WRITING %dx: %s\n", loops, err)
	ioutil.WriteFile("/tmp/firehose_writer_failed_print.log", []byte(errstr), 0644)
	fmt.Fprint(p.writer, errstr)
}

type ToBufferPrinter struct {
	buffer *bytes.Buffer
}

func NewToBufferPrinter() *ToBufferPrinter {
	return &ToBufferPrinter{
		buffer: bytes.NewBuffer(nil),
	}
}

func (p *ToBufferPrinter) Disabled() bool {
	return false
}

func (p *ToBufferPrinter) Print(input ...string) {
	p.buffer.WriteString("FIRE " + strings.Join(input, " ") + "\n")
}

func (p *ToBufferPrinter) Buffer() *bytes.Buffer {
	return p.buffer
}

func Addr(in common.Address) string {
	return hex.EncodeToString(in[:])
}

func Bool(in bool) string {
	if in {
		return "true"
	}

	return "false"
}

func Hash(in common.Hash) string {
	return hex.EncodeToString(in[:])
}

func Hex(in []byte) string {
	if len(in) == 0 {
		return "."
	}

	return hex.EncodeToString(in)
}

func BigInt(in *big.Int) string {
	return Hex(in.Bytes())
}

func Uint(in uint) string {
	return strconv.FormatUint(uint64(in), 10)
}

func Uint8(in uint8) string {
	return strconv.FormatUint(uint64(in), 10)
}

func Uint64(in uint64) string {
	return strconv.FormatUint(in, 10)
}

func JSON(in interface{}) string {
	out, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}

	return string(out)
}

func ReportHeaderComparisonResult(actual *types.Header, expected *types.Header) {
	reportToUser("There is a mismatch between Firehose genesis block and actual chain's stored genesis block, the actual genesis")
	reportToUser("block's hash field extracted from Geth's database does not fit with hash of genesis block generated")
	reportToUser("from Firehose determined genesis config, you might need to provide the correct 'genesis.json' file")
	reportToUser("via --firehose-genesis-file")
	reportToUser("")
	reportToUser("Comparison of the actual Firehose recomputed genesis block <> expected Geth genesis block")

	compareAddress := fieldComparisonReporter(common.Address.String)
	compareHash := fieldComparisonReporter(common.Hash.String)
	compareUint64 := fieldComparisonReporter(func(x uint64) string { return strconv.FormatUint(x, 10) })
	compareBytes := fieldComparisonReporter(hex.EncodeToString)
	compareBigInt := fieldComparisonReporter(func(x *big.Int) string {
		if x == nil {
			return "<nil>"
		} else {
			return x.String()
		}
	})
	compareTxDependency := fieldComparisonReporter(func(x any) string { return fmt.Sprintf("%#v", x) })

	compareHash("Hash", actual.Hash(), expected.Hash())
	compareUint64("Number", actual.Number.Uint64(), expected.Number.Uint64())
	compareHash("ParentHash", actual.ParentHash, expected.ParentHash)
	compareHash("UncleHash", actual.UncleHash, expected.UncleHash)
	compareAddress("Coinbase", actual.Coinbase, expected.Coinbase)
	compareHash("Root", actual.Root, expected.Root)
	compareHash("TxHash", actual.TxHash, expected.TxHash)
	compareHash("ReceiptHash", actual.ReceiptHash, expected.ReceiptHash)
	compareBytes("Bloom", actual.Bloom[:], expected.Bloom[:])
	compareBigInt("Difficulty", actual.Difficulty, expected.Difficulty)
	compareUint64("GasLimit", actual.GasLimit, expected.GasLimit)
	compareUint64("GasUsed", actual.GasUsed, expected.GasUsed)
	compareUint64("Time", actual.Time, expected.Time)
	compareBytes("Extra", actual.Extra, expected.Extra)
	compareHash("MixDigest", actual.MixDigest, expected.MixDigest)
	compareUint64("Nonce", actual.Nonce.Uint64(), expected.Nonce.Uint64())
	compareBigInt("BaseFee", actual.BaseFee, expected.BaseFee)
	compareTxDependency("TxDependency", actual.TxDependency, expected.TxDependency)

	reportToUser("")
}

func fieldComparisonReporter[T any](toString func(x T) string) func(field string, actual T, expected T) {
	return func(field string, actual T, expected T) {
		resolvedActual := toString(actual)
		resolvedExpected := toString(expected)

		sign := "!="
		if resolvedActual == resolvedExpected {
			sign = "=="
		}

		reportToUser("%s [(actual) %s %s %s (expected)]", field, resolvedActual, sign, resolvedExpected)
	}
}

func reportToUser(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
