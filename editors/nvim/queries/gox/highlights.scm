; Go highlights (adapted from tree-sitter-go, MIT) plus gox elements.

; Function calls

(call_expression
  function: (identifier) @function.call)

(call_expression
  function: (identifier) @function.builtin
  (#match? @function.builtin "^(append|cap|clear|close|complex|copy|delete|imag|len|make|max|min|new|panic|print|println|real|recover)$"))

(call_expression
  function: (selector_expression
    field: (field_identifier) @function.method.call))

; Function definitions

(function_declaration
  name: (identifier) @function)

(method_declaration
  name: (field_identifier) @function.method)

; Identifiers

(type_identifier) @type
(field_identifier) @property
(package_identifier) @module
(identifier) @variable

((identifier) @constant
  (#match? @constant "^_*[A-Z][A-Z\\d_]+$"))

; Operators. "<", ">" and "/" are captured by their parent so that tag
; delimiters are never highlighted as operators.

(binary_expression
  operator: _ @operator)

[
  "--"
  "-"
  "-="
  ":="
  "!"
  "*="
  "/="
  "&"
  "&="
  "%="
  "^"
  "^="
  "+"
  "++"
  "+="
  "<-"
  "<<="
  "="
  ">>="
  "|="
  "~"
] @operator

(unary_expression operator: _ @operator)
(pointer_type "*" @operator)
(variadic_parameter_declaration "..." @operator)

; Keywords

[
  "break"
  "case"
  "chan"
  "const"
  "continue"
  "default"
  "defer"
  "else"
  "fallthrough"
  "for"
  "go"
  "goto"
  "if"
  "interface"
  "map"
  "range"
  "select"
  "struct"
  "switch"
  "type"
  "var"
] @keyword

"func" @keyword.function
"return" @keyword.return
["import" "package"] @keyword.import

; Literals

[
  (interpreted_string_literal)
  (raw_string_literal)
] @string

(rune_literal) @character
(escape_sequence) @string.escape

(int_literal) @number
[(float_literal) (imaginary_literal)] @number.float

[(true) (false)] @boolean
[(nil) (iota)] @constant.builtin

(comment) @comment @spell

; Punctuation

["(" ")" "[" "]" "{" "}"] @punctuation.bracket
["," "." ";" ":"] @punctuation.delimiter

; gox elements

; Built-in elements (lowercase) vs components.
((tag_name) @tag.builtin
  (#match? @tag.builtin "^[a-z][a-zA-Z0-9_]*$"))
((tag_name) @tag
  (#match? @tag "[A-Z.]"))

(start_tag ["<" ">"] @tag.delimiter)
(end_tag ["<" "/" ">"] @tag.delimiter)
(self_closing_element ["<" "/" ">"] @tag.delimiter)
(fragment ["<" "/" ">"] @tag.delimiter)

(attribute_name) @tag.attribute
(attribute_string) @string
(spread_attribute "..." @operator)
(spread_attribute ["{" "}"] @punctuation.special)
(expression_container ["{" "}"] @punctuation.special)

(text) @none @spell
