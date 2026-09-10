package payments

import (
	"context"
	"testing"
)

type fakeGateway struct{}

func (fakeGateway) CreateIntent(ctx context.Context, in IntentCommand) (ProviderAck, error) {
	return ProviderAck{Provider: "placeholder", ProviderReference: "intent-" + in.PaymentID.String()}, nil
}

func (fakeGateway) Authorize(ctx context.Context, in PaymentCommand) (ProviderAck, error) {
	return ProviderAck{Provider: "placeholder", ProviderReference: in.ProviderReference}, nil
}

func (fakeGateway) Capture(ctx context.Context, in PaymentCommand) (ProviderAck, error) {
	return ProviderAck{Provider: "placeholder", ProviderReference: in.ProviderReference}, nil
}

func (fakeGateway) Cancel(ctx context.Context, in PaymentCommand) (ProviderAck, error) {
	return ProviderAck{Provider: "placeholder", ProviderReference: in.ProviderReference}, nil
}

func TestGatewayPortIsProviderNeutral(t *testing.T) {
	var g Gateway = fakeGateway{}
	id := mustID(t)
	ack, err := g.CreateIntent(context.Background(), IntentCommand{PaymentID: id, Amount: "10", Currency: "TRY"})
	if err != nil || ack.Provider == "stripe" || ack.Provider == "iyzico" || ack.Provider == "paytr" {
		t.Fatalf("ack = %+v err=%v", ack, err)
	}
}

var _ Gateway = fakeGateway{}
