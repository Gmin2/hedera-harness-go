package mock

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/mirror"
	"github.com/hiero-ledger/hiero-sdk-go/v2/proto/services"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

var nodeServices = []*grpc.ServiceDesc{
	&services.CryptoService_ServiceDesc,
	&services.FileService_ServiceDesc,
	&services.SmartContractService_ServiceDesc,
	&services.ConsensusService_ServiceDesc,
	&services.TokenService_ServiceDesc,
	&services.ScheduleService_ServiceDesc,
	&services.FreezeService_ServiceDesc,
	&services.NetworkService_ServiceDesc,
	&services.UtilService_ServiceDesc,
	&services.AddressBookService_ServiceDesc,
}

func newGRPCServer(l *ledger) *grpc.Server {
	srv := grpc.NewServer(
		// the sdk pings every 10s even without active calls
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	for _, desc := range nodeServices {
		srv.RegisterService(nodeService(l, desc), nil)
	}
	mirror.RegisterConsensusServiceServer(srv, &topicStream{ledger: l})
	return srv
}

// nodeService copies a generated service description and routes every method
// to the ledger. Each method takes either a Transaction or a Query, which is
// read off the generated server interface.
func nodeService(l *ledger, orig *grpc.ServiceDesc) *grpc.ServiceDesc {
	iface := reflect.TypeOf(orig.HandlerType).Elem()
	queryType := reflect.TypeOf(&services.Query{})

	desc := &grpc.ServiceDesc{
		ServiceName: orig.ServiceName,
		HandlerType: orig.HandlerType,
		Metadata:    orig.Metadata,
	}
	for _, m := range orig.Methods {
		goName := strings.ToUpper(m.MethodName[:1]) + m.MethodName[1:]
		method, found := iface.MethodByName(goName)
		isQuery := found && method.Type.NumIn() > 1 && method.Type.In(1) == queryType

		desc.Methods = append(desc.Methods, grpc.MethodDesc{
			MethodName: m.MethodName,
			Handler: func(_ any, ctx context.Context, decode func(any) error, _ grpc.UnaryServerInterceptor) (resp any, err error) {
				// a bug in one handler should fail that call, not take the
				// whole test process down
				defer func() {
					if r := recover(); r != nil {
						resp, err = nil, status.Errorf(codes.Unknown, "mock %s/%s panicked: %v", orig.ServiceName, m.MethodName, r)
					}
				}()
				if isQuery {
					q := new(services.Query)
					if err := decode(q); err != nil {
						return nil, err
					}
					return l.answer(q), nil
				}
				tx := new(services.Transaction)
				if err := decode(tx); err != nil {
					return nil, err
				}
				return &services.TransactionResponse{NodeTransactionPrecheckCode: l.submit(tx)}, nil
			},
		})
	}
	return desc
}
