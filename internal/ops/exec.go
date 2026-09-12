package ops

import (
	"errors"
	"fmt"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// Outcome is what happened to a submitted transaction.
type Outcome struct {
	Status  string // SUCCESS or the failure code, from precheck or receipt
	TxID    string
	Receipt *hiero.TransactionReceipt
	Err     error // transport or sdk problems, not hedera statuses
}

// Execute freezes, signs and submits a built tx, then reads the receipt
// without treating a non success status as an error.
func Execute(env *Env, c Common, built *Tx) (out Outcome) {
	defer func() {
		// the sdk panics when it runs out of healthy nodes
		if r := recover(); r != nil {
			out.Err = fmt.Errorf("sdk panic: %v", r)
		}
	}()

	client := env.Client()
	tx := built.Tx
	var err error

	if c.Memo != "" {
		if tx, err = hiero.TransactionSetTransactionMemo(tx, c.Memo); err != nil {
			return Outcome{Err: err}
		}
	}
	signerRefs := built.Signers
	if c.Payer != "" && c.Payer != "operator" {
		payer, err := env.Actor(c.Payer)
		if err != nil {
			return Outcome{Err: err}
		}
		if tx, err = hiero.TransactionSetTransactionID(tx, hiero.TransactionIDGenerate(payer.ID)); err != nil {
			return Outcome{Err: err}
		}
		signerRefs = append([]string{c.Payer}, signerRefs...)
	}

	if tx, err = hiero.TransactionFreezeWith(tx, client); err != nil {
		return Outcome{Err: fmt.Errorf("freeze: %w", err)}
	}
	signKeys, err := env.SignKeys(signerRefs)
	if err != nil {
		return Outcome{Err: err}
	}
	for _, k := range signKeys {
		if tx, err = hiero.TransactionSign(tx, k); err != nil {
			return Outcome{Err: err}
		}
	}

	txID, _ := hiero.TransactionGetTransactionID(tx)
	out.TxID = txID.String()

	resp, err := hiero.TransactionExecute(tx, client)
	if err != nil {
		var pre hiero.ErrHederaPreCheckStatus
		var rec hiero.ErrHederaReceiptStatus
		switch {
		case errors.As(err, &pre):
			out.Status = pre.Status.String()
		case errors.As(err, &rec):
			// chunked topic submits validate receipts inside Execute
			out.Status = rec.Status.String()
		default:
			out.Err = err
		}
		return out
	}
	out.TxID = resp.TransactionID.String()

	receipt, err := resp.SetValidateStatus(false).GetReceipt(client)
	if err != nil {
		var rec hiero.ErrHederaReceiptStatus
		if errors.As(err, &rec) {
			out.Status = rec.Status.String()
			return out
		}
		out.Err = fmt.Errorf("receipt: %w", err)
		return out
	}
	out.Status = receipt.Status.String()
	out.Receipt = &receipt
	return out
}
