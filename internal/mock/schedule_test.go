package mock

import (
	"testing"

	"github.com/Gmin2/hedera-harness-go/internal/mirror"
	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

func (h *harness) signSchedule(id hiero.ScheduleID, key hiero.PrivateKey) hiero.Status {
	h.t.Helper()
	return h.status(signed(h, hiero.NewScheduleSignTransaction().SetScheduleID(id), key))
}

func TestScheduledTransferExecutesOnSign(t *testing.T) {
	h := newHarness(t)
	alice := h.newAccount(10, 0)
	carol := h.newAccount(10, 0)
	bob := h.newAccount(0, 0)

	inner := hiero.NewTransferTransaction().
		AddHbarTransfer(alice.id, hiero.NewHbar(-1)).
		AddHbarTransfer(carol.id, hiero.NewHbar(-1)).
		AddHbarTransfer(bob.id, hiero.NewHbar(2))
	create, err := hiero.NewScheduleCreateTransaction().SetScheduledTransaction(inner)
	if err != nil {
		t.Fatal(err)
	}
	createResp, err := create.SetScheduleMemo("pay bob").Execute(h.client)
	receipt := h.run(createResp, err)
	scheduleID, scheduledTxID := *receipt.ScheduleID, *receipt.ScheduledTransactionID
	if !scheduledTxID.GetScheduled() {
		t.Fatalf("scheduled transaction id %v is not marked scheduled", scheduledTxID)
	}

	var sched mirror.Schedule
	h.mustRest("/schedules/"+scheduleID.String(), &sched)
	if sched.ExecutedTimestamp != nil || sched.Memo != "pay bob" || sched.PayerAccountID != h.operator.String() {
		t.Fatalf("pending schedule %+v", sched)
	}

	expectStatus(t, h.signSchedule(scheduleID, alice.key), hiero.StatusSuccess)
	expectStatus(t, h.signSchedule(scheduleID, alice.key), hiero.StatusNoNewValidSignatures)
	info, err := hiero.NewScheduleInfoQuery().SetScheduleID(scheduleID).Execute(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if info.ExecutedAt != nil {
		t.Fatal("schedule executed with one of two signatures")
	}

	expectStatus(t, h.signSchedule(scheduleID, carol.key), hiero.StatusSuccess)
	innerReceipt, err := hiero.NewTransactionReceiptQuery().SetTransactionID(scheduledTxID).Execute(h.client)
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, innerReceipt.Status, hiero.StatusSuccess)
	expectStatus(t, h.signSchedule(scheduleID, bob.key), hiero.StatusInvalidScheduleID)

	var acct mirror.Account
	h.mustRest("/accounts/"+bob.id.String(), &acct)
	if acct.Balance.Balance != 2_0000_0000 {
		t.Fatalf("bob balance %d", acct.Balance.Balance)
	}

	h.mustRest("/schedules/"+scheduleID.String(), &sched)
	if sched.ExecutedTimestamp == nil || len(sched.Signatures) != 3 {
		t.Fatalf("executed schedule %+v", sched)
	}

	var scheduled mirror.Transactions
	h.mustRest(mirrorTxPath(createResp.TransactionID)+"?scheduled=true", &scheduled)
	if len(scheduled.Transactions) != 1 {
		t.Fatalf("scheduled transactions %+v", scheduled)
	}
	if got := scheduled.Transactions[0]; got.Name != "CRYPTOTRANSFER" || !got.Scheduled || got.Result != "SUCCESS" {
		t.Fatalf("scheduled transaction %+v", got)
	}
	var all mirror.Transactions
	h.mustRest(mirrorTxPath(createResp.TransactionID), &all)
	if len(all.Transactions) != 2 || all.Transactions[0].Name != "SCHEDULECREATE" {
		t.Fatalf("all transactions for the id %+v", all)
	}
}

func TestIdenticalSchedule(t *testing.T) {
	h := newHarness(t)
	alice := h.newAccount(10, 0)

	schedule := func() (hiero.TransactionReceipt, hiero.Status) {
		inner := hiero.NewTransferTransaction().
			AddHbarTransfer(alice.id, hiero.NewHbar(-1)).
			AddHbarTransfer(h.operator, hiero.NewHbar(1))
		create, err := hiero.NewScheduleCreateTransaction().SetScheduledTransaction(inner)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := create.Execute(h.client)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := resp.SetValidateStatus(false).GetReceipt(h.client)
		if err != nil {
			t.Fatal(err)
		}
		return receipt, receipt.Status
	}

	first, st := schedule()
	expectStatus(t, st, hiero.StatusSuccess)
	second, st := schedule()
	expectStatus(t, st, hiero.StatusIdenticalScheduleAlreadyCreated)
	if second.ScheduleID == nil || *second.ScheduleID != *first.ScheduleID {
		t.Fatalf("identical schedule points at %v, want %v", second.ScheduleID, first.ScheduleID)
	}

	expectStatus(t, h.status(hiero.NewScheduleDeleteTransaction().SetScheduleID(*first.ScheduleID).Execute(h.client)),
		hiero.StatusScheduleIsImmutable)
}

func TestScheduledMintWaitsForSupplyKey(t *testing.T) {
	h := newHarness(t)
	treasury := h.newAccount(10, 0)
	supply, _ := hiero.PrivateKeyGenerateEcdsa()
	token := h.fungibleToken(treasury, func(tx *hiero.TokenCreateTransaction) {
		tx.SetSupplyKey(supply.PublicKey())
	})

	create, err := hiero.NewScheduleCreateTransaction().
		SetScheduledTransaction(hiero.NewTokenMintTransaction().SetTokenID(token).SetAmount(250))
	if err != nil {
		t.Fatal(err)
	}
	receipt := h.run(create.Execute(h.client))

	expectStatus(t, h.signSchedule(*receipt.ScheduleID, supply), hiero.StatusSuccess)
	minted, err := hiero.NewTransactionReceiptQuery().SetTransactionID(*receipt.ScheduledTransactionID).Execute(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if minted.Status != hiero.StatusSuccess || minted.TotalSupply != 10_250 {
		t.Fatalf("scheduled mint receipt %s supply %d", minted.Status, minted.TotalSupply)
	}

	record, err := hiero.NewTransactionRecordQuery().SetTransactionID(*receipt.ScheduledTransactionID).Execute(h.client)
	if err != nil {
		t.Fatal(err)
	}
	if record.ScheduleRef != *receipt.ScheduleID {
		t.Fatalf("record schedule ref %v", record.ScheduleRef)
	}
}
