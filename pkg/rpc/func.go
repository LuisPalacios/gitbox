package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

// Handler serves one method. params is the positional JSON array of the
// request, or nothing when the request had none.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// paramsError marks a failure to decode the parameters of a request. The
// server answers it with CodeInvalidParams.
type paramsError struct{ detail string }

func (e *paramsError) Error() string { return e.detail }

var (
	errorType   = reflect.TypeFor[error]()
	contextType = reflect.TypeFor[context.Context]()
)

// Func turns a Go function into a Handler. The request's positional
// parameters are decoded into the function's arguments, in order; a leading
// context.Context argument receives the request's context instead. The
// function may return nothing, a value, an error, or a value and an error.
//
// Func panics when fn does not have one of those shapes: that is a mistake
// in the program, found when the server is built, not at request time.
func Func(fn any) Handler {
	v := reflect.ValueOf(fn)
	t := v.Type()
	if t.Kind() != reflect.Func || t.IsVariadic() {
		panic(fmt.Sprintf("rpc.Func: %T is not a plain function", fn))
	}
	takesContext := t.NumIn() > 0 && t.In(0) == contextType
	firstArg := 0
	if takesContext {
		firstArg = 1
	}
	var returnsValue, returnsError bool
	switch t.NumOut() {
	case 0:
	case 1:
		returnsError = t.Out(0) == errorType
		returnsValue = !returnsError
	case 2:
		if t.Out(1) != errorType {
			panic(fmt.Sprintf("rpc.Func: the second result of %T must be error", fn))
		}
		returnsValue, returnsError = true, true
	default:
		panic(fmt.Sprintf("rpc.Func: %T returns too many values", fn))
	}
	want := t.NumIn() - firstArg

	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var raw []json.RawMessage
		if len(params) > 0 && string(params) != "null" {
			if err := json.Unmarshal(params, &raw); err != nil {
				return nil, &paramsError{"params must be an array"}
			}
		}
		if len(raw) != want {
			return nil, &paramsError{fmt.Sprintf("expected %d parameters, got %d", want, len(raw))}
		}
		args := make([]reflect.Value, 0, t.NumIn())
		if takesContext {
			args = append(args, reflect.ValueOf(ctx))
		}
		for i, r := range raw {
			arg := reflect.New(t.In(firstArg + i))
			if err := json.Unmarshal(r, arg.Interface()); err != nil {
				return nil, &paramsError{fmt.Sprintf("parameter %d has the wrong type", i+1)}
			}
			args = append(args, arg.Elem())
		}
		out := v.Call(args)
		var result any
		var err error
		if returnsError {
			if e := out[len(out)-1]; !e.IsNil() {
				err = e.Interface().(error)
			}
		}
		if returnsValue {
			result = out[0].Interface()
		}
		return result, err
	}
}
