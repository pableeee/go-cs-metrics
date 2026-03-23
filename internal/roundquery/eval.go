package roundquery

import (
	"fmt"

	"github.com/google/cel-go/cel"
)

// NewEnv builds a CEL environment with all round fields declared as variables.
func NewEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("map", cel.StringType),
		cel.Variable("round_num", cel.IntType),
		cel.Variable("winner", cel.StringType),
		cel.Variable("type_t", cel.StringType),
		cel.Variable("type_ct", cel.StringType),
		cel.Variable("equip_t", cel.IntType),
		cel.Variable("equip_ct", cel.IntType),
		cel.Variable("smokes_t", cel.IntType),
		cel.Variable("smokes_ct", cel.IntType),
		cel.Variable("flashes_t", cel.IntType),
		cel.Variable("flashes_ct", cel.IntType),
		cel.Variable("molotovs_t", cel.IntType),
		cel.Variable("molotovs_ct", cel.IntType),
		cel.Variable("hes_t", cel.IntType),
		cel.Variable("hes_ct", cel.IntType),
		cel.Variable("he_damage_t", cel.IntType),
		cel.Variable("he_damage_ct", cel.IntType),
		cel.Variable("flash_kills_t", cel.IntType),
		cel.Variable("flash_kills_ct", cel.IntType),
		cel.Variable("alive_start_t", cel.IntType),
		cel.Variable("alive_start_ct", cel.IntType),
		cel.Variable("alive_plant_t", cel.IntType),
		cel.Variable("alive_plant_ct", cel.IntType),
		cel.Variable("planted", cel.BoolType),
		cel.Variable("defused", cel.BoolType),
		cel.Variable("exploded", cel.BoolType),
		cel.Variable("entry_side", cel.StringType),
	)
}

// Compile parses and type-checks a CEL expression, returning a ready Program.
// Returns a descriptive error if the expression has syntax or type errors.
func Compile(env *cel.Env, expr string) (cel.Program, error) {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("invalid query: %w", issues.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("build program: %w", err)
	}
	return prg, nil
}

// Matches evaluates the compiled program against a single RoundRecord.
// Returns true if the round satisfies the expression.
func Matches(prg cel.Program, r RoundRecord) (bool, error) {
	out, _, err := prg.Eval(r.celVars())
	if err != nil {
		return false, err
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("query must return a bool, got %T", out.Value())
	}
	return b, nil
}
