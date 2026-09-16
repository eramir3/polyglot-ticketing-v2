package grpc

import (
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"polyglot-ticketing-v2/apps/orders/internal/errorcode"
	"polyglot-ticketing-v2/apps/orders/internal/order"
	commonv1 "polyglot-ticketing-v2/protogen/go/common/v1"
)

// ValidationInterceptor enforces protobuf request constraints before an Orders
// RPC handler runs.
func ValidationInterceptor(validator protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		message, ok := request.(proto.Message)
		if !ok {
			return handler(ctx, request)
		}
		if err := validator.Validate(message); err != nil {
			if validationErrors, ok := protobufValidationErrors(err); ok {
				return nil, structuredError(codes.InvalidArgument, validationErrors)
			}
			return nil, status.Error(codes.Internal, "Unable to validate request.")
		}
		return handler(ctx, request)
	}
}

func protobufValidationErrors(err error) ([]order.ValidationError, bool) {
	var validationError *protovalidate.ValidationError
	if !errors.As(err, &validationError) {
		return nil, false
	}

	validationErrors := make([]order.ValidationError, 0, len(validationError.Violations))
	for _, violation := range validationError.Violations {
		field := ""
		if violation != nil && violation.FieldDescriptor != nil {
			field = violation.FieldDescriptor.JSONName()
		}
		switch field {
		case "ticketId":
			validationErrors = append(validationErrors, invalidArgument(field, "Ticket ID must be a valid UUID."))
		case "orderId":
			validationErrors = append(validationErrors, invalidArgument(field, "Order ID must be a valid UUID."))
		case "userId":
			validationErrors = append(validationErrors, invalidArgument(field, "Order user is required."))
		default:
			validationErrors = append(validationErrors, invalidArgument(field, "Invalid request field."))
		}
	}
	return validationErrors, true
}

func invalidArgument(field string, message string) order.ValidationError {
	return order.ValidationError{
		Code:    errorcode.String(commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT),
		Field:   field,
		Message: message,
	}
}
