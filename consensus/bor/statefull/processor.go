package statefull

import (
	"context"
	"fmt"
	"math"
	"math/big"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/firehose"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

var systemAddress = common.HexToAddress("0xffffFFFfFFffffffffffffffFfFFFfffFFFfFFfE")

type ChainContext struct {
	Chain consensus.ChainHeaderReader
	Bor   consensus.Engine
}

func (c ChainContext) Engine() consensus.Engine {
	return c.Bor
}

func (c ChainContext) GetHeader(hash common.Hash, number uint64) *types.Header {
	return c.Chain.GetHeader(hash, number)
}

// callmsg implements core.Message to allow passing it as a transaction simulator.
type Callmsg struct {
	ethereum.CallMsg
}

func (m Callmsg) From() common.Address { return m.CallMsg.From }
func (m Callmsg) Nonce() uint64        { return 0 }
func (m Callmsg) CheckNonce() bool     { return false }
func (m Callmsg) To() *common.Address  { return m.CallMsg.To }
func (m Callmsg) GasPrice() *big.Int   { return m.CallMsg.GasPrice }
func (m Callmsg) Gas() uint64          { return m.CallMsg.Gas }
func (m Callmsg) Value() *big.Int      { return m.CallMsg.Value }
func (m Callmsg) Data() []byte         { return m.CallMsg.Data }

// get system message
func GetSystemMessage(toAddress common.Address, data []byte) Callmsg {
	return Callmsg{
		ethereum.CallMsg{
			From:     systemAddress,
			Gas:      math.MaxUint64 / 2,
			GasPrice: big.NewInt(0),
			Value:    big.NewInt(0),
			To:       &toAddress,
			Data:     data,
		},
	}
}

var dmFakeBytesV = new(big.Int).Bytes()
var dmFakeBytesR = new(big.Int).Bytes()
var dmFakeBytesS = new(big.Int).Bytes()

// apply message
func ApplyMessage(
	_ context.Context,
	msg Callmsg,
	state *state.StateDB,
	header *types.Header,
	chainConfig *params.ChainConfig,
	chainContext core.ChainContext,
	spanID uint64,
	firehoseContext *firehose.Context,
) (uint64, error) {
	var txHash common.Hash
	if firehoseContext.Enabled() {
		sha := sha3.NewLegacyKeccak256().(crypto.KeccakState)
		sha.Reset()
		rlp.Encode(sha, []interface{}{spanID, msg})
		sha.Read(txHash[:])

		firehoseContext.StartTransactionRaw(
			txHash,
			msg.To(),
			msg.Value(),
			dmFakeBytesV, dmFakeBytesR, dmFakeBytesS,
			msg.Gas(),
			msg.GasPrice(),
			msg.Nonce(),
			msg.Data(),
			// System transaction in Bor engine from `getSystemMessage` are legacy transaction, so we have three nils here
			nil,
			nil,
			nil,
			types.LegacyTxType,
		)
		firehoseContext.RecordTrxFrom(msg.From())
	}

	initialGas := msg.Gas()

	// Create a new context to be used in the EVM environment
	blockContext := core.NewEVMBlockContext(header, chainContext, &header.Coinbase)

	to := "0x0"
	if t := msg.To(); t != nil {
		to = fmt.Sprint(*t)
	}
	fmt.Println("applying message, block/from/to, firehoseContextEnbled?", blockContext.BlockNumber, msg.From(), to, firehoseContext.Enabled())
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(blockContext, vm.TxContext{}, state, chainConfig, vm.Config{}, firehoseContext)

	// Apply the transaction to the current state (included in the env)
	_, gasLeft, err := vmenv.Call(
		vm.AccountRef(msg.From()),
		*msg.To(),
		msg.Data(),
		msg.Gas(),
		msg.Value(),
	)
	// Update the state with pending changes
	if err != nil {
		state.Finalise(true)
	}

	gasUsed := initialGas - gasLeft
	fmt.Println("gas used", gasUsed, "hash", txHash)

	if firehoseContext.Enabled() {
		blockHash := header.Hash()
		cumulativeGasUsed := firehoseContext.CumulativeGasUsed() + gasUsed

		receipt := types.NewReceipt(nil, err != nil, cumulativeGasUsed)
		receipt.TxHash = txHash
		receipt.GasUsed = gasUsed

		// If the transaction created a contract, store the creation address in the receipt.
		if msg.To() == nil {
			receipt.ContractAddress = crypto.CreateAddress(vmenv.TxContext.Origin, spanID)
		}
		// Set the receipt logs and create a bloom for filtering
		receipt.Logs = state.GetLogs(txHash, blockHash)
		receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
		receipt.BlockHash = blockHash
		receipt.BlockNumber = header.Number
		receipt.TransactionIndex = firehoseContext.LastTransactionIndex() + 1
		firehoseContext.EndTransaction(receipt)
	}

	return gasUsed, nil
}

func ApplyBorMessage(vmenv vm.EVM, msg Callmsg) (*core.ExecutionResult, error) {
	initialGas := msg.Gas()

	fmt.Println("applying BOR message, block/from", vmenv.Context.BlockNumber, msg.From())
	// Apply the transaction to the current state (included in the env)
	ret, gasLeft, err := vmenv.Call(
		vm.AccountRef(msg.From()),
		*msg.To(),
		msg.Data(),
		msg.Gas(),
		msg.Value(),
	)
	// Update the state with pending changes
	if err != nil {
		vmenv.StateDB.Finalise(true)
	}

	gasUsed := initialGas - gasLeft

	return &core.ExecutionResult{
		UsedGas:    gasUsed,
		Err:        err,
		ReturnData: ret,
	}, nil
}
