package diag

import (
	"fmt"
)

type Code uint16

const (
	// Неизвестная ошибка - на первое время
	UnknownCode Code = 0
	// Лексические
	LexInfo                     Code = 1000
	LexUnknownChar              Code = 1001
	LexUnterminatedString       Code = 1002
	LexUnterminatedBlockComment Code = 1003
	LexBadNumber                Code = 1004
	LexTokenTooLong             Code = 1005

	// Парсерные (зарезервируем)
	SynInfo                    Code = 2000
	SynUnexpectedToken         Code = 2001
	SynUnclosedDelimiter       Code = 2002
	SynUnclosedBlockComment    Code = 2003
	SynUnclosedString          Code = 2004
	SynUnclosedChar            Code = 2005
	SynUnclosedParen           Code = 2006
	SynUnclosedBrace           Code = 2007
	SynUnclosedBracket         Code = 2008
	SynUnclosedSquareBracket   Code = 2009
	SynUnclosedAngleBracket    Code = 2010
	SynUnclosedCurlyBracket    Code = 2011
	SynExpectSemicolon         Code = 2012
	SynForMissingIn            Code = 2013
	SynForBadHeader            Code = 2014
	SynModifierNotAllowed      Code = 2015
	SynAttributeNotAllowed     Code = 2016
	SynAsyncNotAllowed         Code = 2017
	SynTypeExpectEquals        Code = 2018
	SynTypeExpectBody          Code = 2019
	SynTypeExpectUnionMember   Code = 2020
	SynTypeFieldConflict       Code = 2021
	SynTypeDuplicateMember     Code = 2022
	SynTypeNotAllowed          Code = 2023
	SynEnumExpectBody          Code = 2024
	SynEnumExpectRBrace        Code = 2025
	SynIllegalItemInExtern     Code = 2026
	SynVisibilityReduction     Code = 2027
	SynFatArrowOutsideParallel Code = 2028
	SynPragmaPosition          Code = 2029
	SynFnNotAllowed            Code = 2030

	// import errors & warnings
	SynInfoImportGroup    Code = 2100
	SynUnexpectedTopLevel Code = 2101
	SynExpectIdentifier   Code = 2102
	SynExpectModuleSeg    Code = 2103
	SynExpectItemAfterDbl Code = 2104
	SynExpectIdentAfterAs Code = 2105
	SynEmptyImportGroup   Code = 2106

	// type errors & warnings
	SynInfoTypeExpr       Code = 2200
	SynExpectRightBracket Code = 2201
	SynExpectType         Code = 2202
	SynExpectExpression   Code = 2203
	SynExpectColon        Code = 2204
	SynUnexpectedModifier Code = 2205
	SynInvalidTupleIndex  Code = 2206
	SynVariadicMustBeLast Code = 2207

	// Семантические (резервируем)
	SemaInfo                       Code = 3000
	SemaError                      Code = 3001
	SemaDuplicateSymbol            Code = 3002
	SemaScopeMismatch              Code = 3003
	SemaShadowSymbol               Code = 3004
	SemaUnresolvedSymbol           Code = 3005
	SemaFnOverride                 Code = 3006
	SemaIntrinsicBadContext        Code = 3007
	SemaIntrinsicBadName           Code = 3008
	SemaIntrinsicHasBody           Code = 3009
	SemaAmbiguousCtorOrFn          Code = 3010
	SemaFnNameStyle                Code = 3011
	SemaTagNameStyle               Code = 3012
	SemaModuleMemberNotFound       Code = 3013
	SemaModuleMemberNotPublic      Code = 3014
	SemaTypeMismatch               Code = 3015
	SemaInvalidBinaryOperands      Code = 3016
	SemaInvalidUnaryOperand        Code = 3017
	SemaBorrowConflict             Code = 3018
	SemaBorrowMutation             Code = 3019
	SemaBorrowMove                 Code = 3020
	SemaBorrowThreadEscape         Code = 3021
	SemaBorrowImmutable            Code = 3022
	SemaBorrowNonAddressable       Code = 3023
	SemaBorrowDropInvalid          Code = 3024
	SemaExpectTypeOperand          Code = 3025
	SemaConstNotConstant           Code = 3026
	SemaConstCycle                 Code = 3027
	SemaContractDuplicateField     Code = 3028
	SemaContractDuplicateMethod    Code = 3029
	SemaContractMethodBody         Code = 3030
	SemaContractSelfType           Code = 3031
	SemaContractUnusedTypeParam    Code = 3032
	SemaContractUnknownAttr        Code = 3033
	SemaContractBoundNotFound      Code = 3034
	SemaContractBoundNotContract   Code = 3035
	SemaContractBoundDuplicate     Code = 3036
	SemaContractBoundTypeError     Code = 3037
	SemaContractMissingField       Code = 3038
	SemaContractFieldTypeError     Code = 3039
	SemaContractMissingMethod      Code = 3040
	SemaContractMethodMismatch     Code = 3041
	SemaContractFieldAttrMismatch  Code = 3042
	SemaContractMethodAttrMismatch Code = 3043
	SemaExternDuplicateField       Code = 3044
	SemaExternUnknownAttr          Code = 3045
	SemaNoOverload                 Code = 3046
	SemaAmbiguousOverload          Code = 3047
	SemaWildcardValue              Code = 3048
	SemaWildcardMut                Code = 3049
	SemaInvalidBoolContext         Code = 3050
	SemaMissingReturn              Code = 3051
	SemaNoStdlib                   Code = 3052
	SemaNonexhaustiveMatch         Code = 3053
	SemaRedundantFinally           Code = 3054
	SemaHiddenPublic               Code = 3055
	SemaEntrypointNotFound         Code = 3056
	SemaMultipleEntrypoints        Code = 3057
	SemaEntrypointNoBody           Code = 3058
	SemaEntrypointInvalidAttr      Code = 3059

	// Attribute validation (3060-3076)
	SemaAttrConflict             Code = 3060 // General attribute conflict
	SemaAttrPackedAlign          Code = 3061 // @packed conflicts with @align
	SemaAttrSendNosend           Code = 3062 // @send conflicts with @nosend
	SemaAttrNonblockingWaitsOn   Code = 3063 // @nonblocking conflicts with @waits_on
	SemaAttrAlignNotPowerOfTwo   Code = 3064 // @align(N) where N is not power of 2
	SemaAttrAlignInvalidValue    Code = 3065 // @align with non-numeric argument
	SemaAttrBackendUnknown       Code = 3066 // @backend with unknown target
	SemaAttrBackendInvalidArg    Code = 3067 // @backend with non-string argument
	SemaAttrGuardedByNotField    Code = 3068 // @guarded_by references non-existent field
	SemaAttrGuardedByNotLock     Code = 3069 // @guarded_by field is not Mutex/RwLock
	SemaAttrRequiresLockNotField Code = 3070 // @requires_lock references non-existent field
	SemaAttrWaitsOnNotField      Code = 3071 // @waits_on references non-existent field
	SemaAttrMissingParameter     Code = 3072 // Required parameter missing
	SemaAttrInvalidParameter     Code = 3073 // Invalid parameter value/type
	SemaAttrSealedExtend         Code = 3074 // Cannot extend @sealed type
	SemaAttrReadonlyWrite        Code = 3075 // Cannot write to @readonly field
	SemaAttrPureViolation        Code = 3076 // @pure function has side effects

	// Lock analysis and concurrency contracts (3077-3089)
	SemaLockGuardedByViolation   Code = 3077 // Accessing @guarded_by field without lock
	SemaLockRequiresNotHeld      Code = 3078 // Calling @requires_lock without holding
	SemaLockDoubleAcquire        Code = 3079 // Acquiring already-held lock
	SemaLockReleaseNotHeld       Code = 3080 // Releasing lock not held
	SemaLockNotReleasedOnExit    Code = 3081 // @acquires_lock but not released
	SemaLockUnbalanced           Code = 3082 // Lock state differs between branches
	SemaLockUnverified           Code = 3083 // Cannot verify lock held (warning)
	SemaLockNonblockingCallsWait Code = 3084 // @nonblocking calls @waits_on
	SemaLockFieldNotLockType     Code = 3085 // Referenced field not Mutex/RwLock
	SemaNosendInSpawn            Code = 3086 // @nosend type in spawn
	SemaFailfastNonAsync         Code = 3087 // @failfast on non-async block
	SemaLockAcquiresNotField     Code = 3088 // @acquires_lock refs non-existent field
	SemaLockReleasesNotField     Code = 3089 // @releases_lock refs non-existent field
	SemaIteratorNotImplemented   Code = 3090 // Type does not implement iterator (__range)
	SemaRangeTypeMismatch        Code = 3091 // Range operands have incompatible types
	SemaIndexOutOfBounds         Code = 3092 // Index out of bounds

	// Enum validation (3093-3097)
	SemaEnumVariantNotFound   Code = 3093 // Enum variant does not exist
	SemaEnumValueOverflow     Code = 3094 // Enum value overflow
	SemaEnumValueTypeMismatch Code = 3095 // Enum value type mismatch
	SemaEnumDuplicateVariant  Code = 3096 // Duplicate enum variant name
	SemaEnumInvalidBaseType   Code = 3097 // Invalid base type for enum

	// Implicit conversion errors (3098-3099)
	SemaNoConversion        Code = 3098 // No conversion from T to U
	SemaAmbiguousConversion Code = 3099 // Ambiguous conversion from T to U

	// Additional concurrency attribute validation (3100-3101)
	SemaAttrWaitsOnNotCondition Code = 3100 // @waits_on field must be Condition/Semaphore
	SemaAttrAtomicInvalidType   Code = 3101 // @atomic field must be int/uint/bool/*T

	// Deadlock detection (3102)
	SemaLockPotentialDeadlock Code = 3102 // Potential deadlock: lock order cycle detected

	// Channel errors (3103-3106)
	SemaChannelTypeMismatch          Code = 3103 // send/recv type doesn't match channel<T>
	SemaChannelSendAfterClose        Code = 3104 // Attempt to send on closed channel
	SemaChannelNosendValue           Code = 3105 // Cannot send @nosend type through channel
	SemaChannelBlockingInNonblocking Code = 3106 // Blocking channel op in @nonblocking

	// Task leak detection (3107-3110)
	SemaTaskNotAwaited    Code = 3107 // Task spawned but not awaited
	SemaTaskEscapesScope  Code = 3108 // Task stored in global without detach
	SemaTaskLeakInAsync   Code = 3109 // Unawaited task at async block exit
	SemaTaskLifetimeError Code = 3110 // Task outlives spawning scope
	SemaSpawnNotTask      Code = 3111 // spawn requires Task<T> expression

	// Generic type parameter errors (3112)
	SemaTypeParamShadow Code = 3112 // Type parameter shadows outer type parameter in extern

	// @send/@atomic validation (3113-3114)
	SemaSendContainsNonsend Code = 3113 // @send type contains non-sendable field
	SemaAtomicDirectAccess  Code = 3114 // @atomic field accessed without atomic operation

	// Spawn warnings (3115)
	SemaSpawnCheckpointUseless Code = 3115 // spawn checkpoint() has no effect

	// Clone errors (3116)
	SemaTypeNotClonable Code = 3116 // Type does not have __clone method

	// @copy attribute errors (3117-3118)
	SemaAttrCopyNonCopyField Code = 3117 // @copy type has non-Copy field
	SemaAttrCopyCyclicDep    Code = 3118 // @copy types have cyclic dependency

	// Directive validation errors (3119-3120)
	SemaDirectiveUnknownNamespace   Code = 3119 // Directive namespace not imported
	SemaDirectiveNotDirectiveModule Code = 3120 // Module lacks pragma directive

	// Entrypoint validation errors (3121-3125)
	SemaEntrypointModeInvalid          Code = 3121 // Unknown entrypoint mode string
	SemaEntrypointNoModeRequiresNoArgs Code = 3122 // @entrypoint without mode requires 0 params or all defaults
	SemaEntrypointReturnNotConvertible Code = 3123 // Return type not convertible to int (no ExitCode)
	SemaEntrypointParamNoFromArgv      Code = 3124 // Parameter lacks FromArgv implementation
	SemaEntrypointParamNoFromStdin     Code = 3125 // Parameter lacks FromStdin implementation
	SemaRecursiveUnsized               Code = 3126 // Recursive value type has infinite size
	SemaDeprecatedUsage                Code = 3127 // Usage of deprecated element (warning)
	SemaIntLiteralOutOfRange           Code = 3128 // Integer literal out of range for target type
	SemaRawPointerNotAllowed           Code = 3129 // Raw pointer types are backend-only
	SemaUseAfterMove                   Code = 3130 // Use of moved value
	SemaTrivialRecursion               Code = 3131 // Obvious infinite recursion cycle

	// Ошибки I/O
	IOLoadFileError Code = 4001

	// Ошибки проекта / DAG
	ProjInfo                    Code = 5000
	ProjDuplicateModule         Code = 5001
	ProjMissingModule           Code = 5002
	ProjSelfImport              Code = 5003
	ProjImportCycle             Code = 5004
	ProjInvalidModulePath       Code = 5005
	ProjInvalidImportPath       Code = 5006
	ProjDependencyFailed        Code = 5007
	ProjMissingModulePragma     Code = 5008
	ProjInconsistentModuleName  Code = 5009
	ProjWrongModuleNameInImport Code = 5010
	ProjInconsistentNoStd       Code = 5011

	// Observability
	ObsInfo    Code = 6000
	ObsTimings Code = 6001

	// Future/Unsupported Features (v2+)
	FutSignalNotSupported         Code = 7000
	FutParallelNotSupported       Code = 7001
	FutMacroNotSupported          Code = 7002
	FutEntrypointModeEnv          Code = 7003 // @entrypoint("env") reserved for future
	FutEntrypointModeConfig       Code = 7004 // @entrypoint("config") reserved for future
	FutNullCoalescingNotSupported Code = 7005
	FutNestedFnNotSupported       Code = 7006

	// Alien hints (8000-series; optional extra diagnostics)
	AlnRustImplTrait   Code = 8001
	AlnRustAttribute   Code = 8002
	AlnRustMacroCall   Code = 8003
	AlnGoDefer         Code = 8004
	AlnTSInterface     Code = 8005
	AlnRustImplicitRet Code = 8006
	AlnPythonNoneType  Code = 8010
	AlnPythonNoneAlias Code = 8050
)

var ( // todo расширить описания и использовать как notes
	codeDescription = map[Code]string{
		UnknownCode:                        "Unknown error",
		LexInfo:                            "Lexical information",
		LexUnknownChar:                     "Unknown character",
		LexUnterminatedString:              "Unterminated string",
		LexUnterminatedBlockComment:        "Unterminated block comment",
		LexBadNumber:                       "Bad number",
		LexTokenTooLong:                    "Token too long",
		SynInfo:                            "Syntax information",
		SynUnexpectedToken:                 "Unexpected token",
		SynUnclosedDelimiter:               "Unclosed delimiter",
		SynUnclosedBlockComment:            "Unclosed block comment",
		SynUnclosedString:                  "Unclosed string",
		SynUnclosedChar:                    "Unclosed char",
		SynUnclosedParen:                   "Unclosed parenthesis",
		SynUnclosedBrace:                   "Unclosed brace",
		SynUnclosedBracket:                 "Unclosed bracket",
		SynUnclosedSquareBracket:           "Unclosed square bracket",
		SynUnclosedAngleBracket:            "Unclosed angle bracket",
		SynUnclosedCurlyBracket:            "Unclosed curly bracket",
		SynInfoImportGroup:                 "Import group information",
		SynUnexpectedTopLevel:              "Unexpected top level",
		SynExpectSemicolon:                 "Expect semicolon",
		SynForMissingIn:                    "Missing 'in' in for-in loop",
		SynForBadHeader:                    "Malformed for-loop header",
		SynModifierNotAllowed:              "Modifier not allowed here",
		SynAttributeNotAllowed:             "Attribute not allowed here",
		SynAsyncNotAllowed:                 "'async' not allowed here",
		SynTypeExpectEquals:                "Expected '=' in type declaration",
		SynTypeExpectBody:                  "Expected type body",
		SynTypeExpectUnionMember:           "Expected union member",
		SynTypeFieldConflict:               "Duplicate field in type",
		SynTypeDuplicateMember:             "Duplicate union member",
		SynTypeNotAllowed:                  "Type declaration is not allowed here",
		SynEnumExpectBody:                  "Expected '{' for enum body",
		SynEnumExpectRBrace:                "Expected '}' after enum body",
		SynIllegalItemInExtern:             "Illegal item inside extern block",
		SynVisibilityReduction:             "Visibility reduction is not allowed",
		SynFatArrowOutsideParallel:         "Fat arrow is only allowed in parallel expressions, compare arms, or select/race arms",
		SynPragmaPosition:                  "Pragma must appear at the top of the file",
		SynFnNotAllowed:                    "Function declaration is not allowed here",
		SynExpectIdentifier:                "Expect identifier",
		SynExpectModuleSeg:                 "Expect module segment",
		SynExpectItemAfterDbl:              "Expect item after double colon",
		SynExpectIdentAfterAs:              "Expect identifier after as",
		SynEmptyImportGroup:                "Empty import group",
		SynInfoTypeExpr:                    "Type expression information",
		SynExpectRightBracket:              "Expect right bracket",
		SynExpectType:                      "Expect type",
		SynExpectExpression:                "Expect expression",
		SynExpectColon:                     "Expect colon",
		SynUnexpectedModifier:              "Unexpected modifier",
		SynInvalidTupleIndex:               "Invalid tuple index",
		SynVariadicMustBeLast:              "Variadic parameter must be last",
		SemaInfo:                           "Semantic information",
		SemaError:                          "Semantic error",
		SemaDuplicateSymbol:                "Duplicate symbol",
		SemaScopeMismatch:                  "Scope stack mismatch",
		SemaShadowSymbol:                   "Shadowed symbol",
		SemaUnresolvedSymbol:               "Unresolved symbol",
		SemaFnOverride:                     "Invalid function override",
		SemaIntrinsicBadContext:            "Intrinsic declaration outside allowed module",
		SemaIntrinsicBadName:               "Invalid intrinsic name",
		SemaIntrinsicHasBody:               "Intrinsic must not have a body",
		SemaAmbiguousCtorOrFn:              "Ambiguous constructor or function call",
		SemaFnNameStyle:                    "Function name style warning",
		SemaTagNameStyle:                   "Tag name style warning",
		SemaModuleMemberNotFound:           "Module member not found",
		SemaModuleMemberNotPublic:          "Module member is not public",
		SemaTypeMismatch:                   "Type mismatch",
		SemaInvalidBinaryOperands:          "Invalid operands for binary operator",
		SemaInvalidUnaryOperand:            "Invalid operand for unary operator",
		SemaBorrowConflict:                 "Borrow conflict",
		SemaBorrowMutation:                 "Mutation while borrowed",
		SemaBorrowMove:                     "Move while borrowed",
		SemaBorrowThreadEscape:             "Borrow escapes thread boundary",
		SemaBorrowImmutable:                "Cannot take mutable borrow of immutable value",
		SemaBorrowNonAddressable:           "Expression is not addressable",
		SemaBorrowDropInvalid:              "Drop target has no active borrow",
		SemaExpectTypeOperand:              "Expected type operand",
		SemaConstNotConstant:               "Const initializer is not constant",
		SemaConstCycle:                     "Const cycle detected",
		SemaContractDuplicateField:         "Duplicate field in contract",
		SemaContractDuplicateMethod:        "Duplicate method in contract",
		SemaContractMethodBody:             "Contract method must not have a body",
		SemaContractSelfType:               "Contract method self parameter mismatch",
		SemaContractUnusedTypeParam:        "Unused contract type parameter",
		SemaContractUnknownAttr:            "Unknown contract attribute",
		SemaContractBoundNotFound:          "Contract in bound not found",
		SemaContractBoundNotContract:       "Identifier in bound is not a contract",
		SemaContractBoundDuplicate:         "Duplicate contract in bounds",
		SemaContractBoundTypeError:         "Invalid contract type argument",
		SemaContractMissingField:           "Missing required contract field",
		SemaContractFieldTypeError:         "Contract field type mismatch",
		SemaContractMissingMethod:          "Missing required contract method",
		SemaContractMethodMismatch:         "Contract method signature mismatch",
		SemaContractFieldAttrMismatch:      "Contract field attribute mismatch",
		SemaContractMethodAttrMismatch:     "Contract method attribute/modifier mismatch",
		SemaExternDuplicateField:           "Duplicate extern field",
		SemaExternUnknownAttr:              "Unsupported extern attribute",
		SemaNoOverload:                     "No matching overload found",
		SemaAmbiguousOverload:              "Ambiguous overload resolution",
		SemaWildcardValue:                  "Wildcard used as value",
		SemaWildcardMut:                    "Wildcard mutability",
		SemaInvalidBoolContext:             "Invalid boolean context",
		SemaMissingReturn:                  "Missing return in function",
		SemaNoStdlib:                       "stdlib not available in no_std module",
		SemaNonexhaustiveMatch:             "non-exhaustive pattern match",
		SemaRedundantFinally:               "redundant finally clause",
		SemaHiddenPublic:                   "@hidden conflicts with pub",
		SemaEntrypointNotFound:             "Entrypoint not found",
		SemaMultipleEntrypoints:            "Multiple entrypoints",
		SemaEntrypointNoBody:               "Entrypoint requires a body",
		SemaEntrypointInvalidAttr:          "Invalid entrypoint attribute usage",
		SemaAttrConflict:                   "Attribute conflict",
		SemaAttrPackedAlign:                "@packed conflicts with @align",
		SemaAttrSendNosend:                 "@send conflicts with @nosend",
		SemaAttrNonblockingWaitsOn:         "@nonblocking conflicts with @waits_on",
		SemaAttrAlignNotPowerOfTwo:         "@align argument must be a power of 2",
		SemaAttrAlignInvalidValue:          "@align requires a numeric argument",
		SemaAttrBackendUnknown:             "@backend target unknown",
		SemaAttrBackendInvalidArg:          "@backend requires a string argument",
		SemaAttrGuardedByNotField:          "@guarded_by references non-existent field",
		SemaAttrGuardedByNotLock:           "@guarded_by field must be Mutex or RwLock",
		SemaAttrRequiresLockNotField:       "@requires_lock references non-existent field",
		SemaAttrWaitsOnNotField:            "@waits_on references non-existent field",
		SemaAttrMissingParameter:           "Attribute parameter missing",
		SemaAttrInvalidParameter:           "Invalid attribute parameter",
		SemaAttrSealedExtend:               "Cannot extend @sealed type",
		SemaAttrReadonlyWrite:              "Cannot write to @readonly field",
		SemaAttrPureViolation:              "@pure function has side effects",
		SemaLockGuardedByViolation:         "accessing @guarded_by field without holding lock",
		SemaLockRequiresNotHeld:            "calling function that requires lock without holding it",
		SemaLockDoubleAcquire:              "attempting to acquire lock already held",
		SemaLockReleaseNotHeld:             "attempting to release lock not currently held",
		SemaLockNotReleasedOnExit:          "lock acquired but not released before function exit",
		SemaLockUnbalanced:                 "lock state differs between branches",
		SemaLockUnverified:                 "cannot statically verify lock is held",
		SemaLockNonblockingCallsWait:       "@nonblocking context calls function that may block",
		SemaLockFieldNotLockType:           "lock field is not Mutex or RwLock type",
		SemaNosendInSpawn:                  "cannot send @nosend type to spawned task",
		SemaFailfastNonAsync:               "@failfast can only be applied to async blocks",
		SemaLockAcquiresNotField:           "@acquires_lock references non-existent field",
		SemaLockReleasesNotField:           "@releases_lock references non-existent field",
		SemaIteratorNotImplemented:         "type does not implement iterator (missing __range method)",
		SemaRangeTypeMismatch:              "range operands have incompatible types",
		SemaIndexOutOfBounds:               "index out of bounds",
		SemaEnumVariantNotFound:            "enum variant not found",
		SemaEnumValueOverflow:              "enum value overflow",
		SemaEnumValueTypeMismatch:          "enum value type mismatch",
		SemaEnumDuplicateVariant:           "duplicate enum variant name",
		SemaEnumInvalidBaseType:            "invalid base type for enum",
		SemaNoConversion:                   "no conversion from source to target type",
		SemaAmbiguousConversion:            "ambiguous conversion from source to target type",
		SemaAttrWaitsOnNotCondition:        "@waits_on field must be Condition or Semaphore",
		SemaAttrAtomicInvalidType:          "@atomic field must be int, uint, bool, or *T",
		SemaLockPotentialDeadlock:          "potential deadlock: lock acquisition order cycle detected",
		SemaChannelTypeMismatch:            "send/recv type doesn't match channel<T>",
		SemaChannelSendAfterClose:          "attempt to send on closed channel",
		SemaChannelNosendValue:             "cannot send @nosend type through channel",
		SemaChannelBlockingInNonblocking:   "blocking channel operation in @nonblocking function",
		SemaTaskNotAwaited:                 "spawned task is neither awaited nor returned",
		SemaTaskEscapesScope:               "cannot store Task<T> in global variable without detach",
		SemaTaskLeakInAsync:                "unawaited task at async block exit",
		SemaTaskLifetimeError:              "task outlives spawning scope",
		SemaSpawnNotTask:                   "spawn requires Task<T> expression",
		SemaSendContainsNonsend:            "@send type contains non-sendable field",
		SemaAtomicDirectAccess:             "@atomic field must be accessed via atomic operations",
		SemaTypeParamShadow:                "type parameter shadows outer type parameter",
		SemaTypeNotClonable:                "type is not clonable (no __clone method)",
		SemaAttrCopyNonCopyField:           "@copy type has non-Copy field",
		SemaAttrCopyCyclicDep:              "@copy types have cyclic dependency",
		SemaDirectiveUnknownNamespace:      "directive namespace is not an imported module",
		SemaDirectiveNotDirectiveModule:    "directive namespace module lacks 'pragma directive'",
		SemaEntrypointModeInvalid:          "unknown @entrypoint mode; valid modes are 'argv', 'stdin'",
		SemaEntrypointNoModeRequiresNoArgs: "@entrypoint without mode requires function callable with no arguments",
		SemaEntrypointReturnNotConvertible: "@entrypoint return type must be 'nothing' or convertible to int",
		SemaEntrypointParamNoFromArgv:      "parameter type does not implement FromArgv contract",
		SemaEntrypointParamNoFromStdin:     "parameter type does not implement FromStdin contract",
		SemaRecursiveUnsized:               "recursive value type has infinite size",
		SemaDeprecatedUsage:                "usage of deprecated element",
		SemaIntLiteralOutOfRange:           "integer literal out of range",
		SemaRawPointerNotAllowed:           "raw pointers are backend-only",
		SemaUseAfterMove:                   "use of moved value",
		SemaTrivialRecursion:               "obvious infinite recursion cycle",
		IOLoadFileError:                    "I/O load file error",
		ProjInfo:                           "Project information",
		ProjDuplicateModule:                "Duplicate module definition",
		ProjMissingModule:                  "Missing module",
		ProjSelfImport:                     "Module imports itself",
		ProjImportCycle:                    "Import cycle detected",
		ProjInvalidModulePath:              "Invalid module path",
		ProjInvalidImportPath:              "Invalid import path",
		ProjDependencyFailed:               "Dependency module has errors",
		ProjMissingModulePragma:            "Missing module pragma",
		ProjInconsistentModuleName:         "Inconsistent module name within directory",
		ProjWrongModuleNameInImport:        "Wrong module name in import",
		ProjInconsistentNoStd:              "Inconsistent no_std pragmas in module",
		ObsInfo:                            "Observability information",
		ObsTimings:                         "Pipeline timings",
		FutSignalNotSupported:              "'signal' is not supported in v1, reserved for future use",
		FutParallelNotSupported:            "'parallel' requires multi-threading (v2+)",
		FutMacroNotSupported:               "'macro' is planned for v2+",
		FutEntrypointModeEnv:               "@entrypoint(\"env\") mode is reserved for future use",
		FutEntrypointModeConfig:            "@entrypoint(\"config\") mode is reserved for future use",
		FutNullCoalescingNotSupported:      "null coalescing '??' is not supported in v1",
		FutNestedFnNotSupported:            "nested function declarations are not supported yet",
		AlnRustImplTrait:                   "alien hint: rust impl/trait",
		AlnRustAttribute:                   "alien hint: rust attribute syntax",
		AlnRustMacroCall:                   "alien hint: rust macro call",
		AlnRustImplicitRet:                 "alien hint: rust implicit return",
		AlnGoDefer:                         "alien hint: go defer",
		AlnTSInterface:                     "alien hint: typescript interface",
		AlnPythonNoneType:                  "alien hint: python None type",
		AlnPythonNoneAlias:                 "alien hint: python None alias",
	}
)

func (c Code) ID() string {
	switch ic := int(c); {
	case ic >= 1000 && ic < 2000:
		return fmt.Sprintf("LEX%04d", ic)
	case ic >= 2000 && ic < 3000:
		return fmt.Sprintf("SYN%04d", ic)
	case ic >= 3000 && ic < 4000:
		return fmt.Sprintf("SEM%04d", ic)
	case ic >= 4000 && ic < 5000:
		return fmt.Sprintf("IO%04d", ic)
	case ic >= 5000 && ic < 6000:
		return fmt.Sprintf("PRJ%04d", ic)
	case ic >= 6000 && ic < 7000:
		return fmt.Sprintf("OBS%04d", ic)
	case ic >= 7000 && ic < 8000:
		return fmt.Sprintf("FUT%04d", ic)
	case ic >= 8000 && ic < 9000:
		return fmt.Sprintf("ALN%04d", ic)
	}
	return "E0000"
}

func (c Code) Title() string {
	desc, ok := codeDescription[c]
	if !ok {
		return codeDescription[Code(0)]
	}
	return desc
}

func (c Code) String() string {
	return fmt.Sprintf("[%s]: %s", c.ID(), c.Title())
}
