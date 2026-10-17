package investigator

import (
	"reflect"
	"unicode/utf8"

	"github.com/eino-contrib/jsonschema"
	"vidlens/internal/domain"
)

// The exported alias keeps the original flattened fields and their historical
// schema intact. Only newly created v5 calls accept activity metadata.
type ObservationFields = observation

const observationStringMax = 1024
const observationPayloadMax = 32 << 10

type pacedObservation struct {
	ObservationFields
	ActivityTitle string `json:"activity_title,omitempty" jsonschema:"description=Optional short Chinese activity label. It is metadata, not video evidence."`
}

func observationSchema(name string, typ reflect.Type, _ reflect.StructTag, sc *jsonschema.Schema) {
	if name == "frame_ids" && sc.Type == "array" {
		sc.MinItems, sc.MaxItems = domain.Ptr(uint64(1)), domain.Ptr(uint64(12))
	}
	switch name {
	case "visible_facts", "literal_details", "inferences", "conflicts", "unresolved":
		if sc.Type == "array" {
			sc.MaxItems = domain.Ptr(uint64(16))
		}
		if typ.Kind() == reflect.String {
			sc.MaxLength = domain.Ptr(uint64(observationStringMax))
		}
	}
}

func validObservationSize(in observation) bool {
	if len(domain.JSON(in)) > observationPayloadMax {
		return false
	}
	for _, items := range [][]string{in.VisibleFacts, in.LiteralDetails, in.Inferences, in.Conflicts, in.Unresolved} {
		if len(items) > 16 {
			return false
		}
		for _, text := range items {
			if utf8.RuneCountInString(text) > observationStringMax {
				return false
			}
		}
	}
	return true
}

const observationBudgetInstructions = `
record_observation用于简洁观察记录：每个字段最多16项，每项最多1024个字符，整个记录最多32KiB。表达应简洁，不要逐字抄录整屏终端、文件列表或无关提交ID。一次概览通常用少量主题事实和必要的文字/画面冲突即可；截图中的全部文字不必变成literal_details。保留影响主要结论的数字、条件和可辨认原文；读不清的细节列为未确认，不能猜测。activity_title是可选动作元信息，不是事实。输出必须是完整JSON对象，预留结束引号、数组和对象的空间。`
