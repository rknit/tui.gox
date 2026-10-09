# tree-sitter-gox

A tree-sitter grammar for `.gox` files: Go with TSX-style XML elements. It extends
[tree-sitter-go](https://github.com/tree-sitter/tree-sitter-go), so every Go node keeps its usual
name. The elements add these nodes:

| Node | Example |
|---|---|
| `element` (`start_tag`, children, `end_tag`) | `<box>…</box>` |
| `self_closing_element` | `<Card title="x" />` |
| `fragment` | `<>…</>` |
| `tag_name` | `box`, `Card`, `ui.Card` (generic arguments are `type_arguments`) |
| `attribute` (`attribute_name`, `value`) | `padding={1}`, `border="rounded"`, `hidden` |
| `attribute_string` | `"rounded"` |
| `spread_attribute` | `{...props}` |
| `expression_container` | `{count}`, `{/* comment */}` |
| `text` | text between tags |

`queries/highlights.scm` covers Go and the elements. It uses the standard capture names:
`@tag`, `@tag.builtin`, `@tag.attribute`, `@tag.delimiter` and so on.

The generated parser is committed in `src/`, so compiling it needs only a C compiler. It uses
ABI 14 so that Neovim 0.9 and later can load it.

To work on the grammar:

```sh
npm install
npm run generate   # tree-sitter generate --abi 14
npm test           # corpus tests in test/corpus
```
