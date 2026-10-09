" Vim syntax file for .gox (Go with XML elements).
if exists("b:current_syntax")
  finish
endif

runtime! syntax/go.vim
unlet! b:current_syntax

" Tags: <box, </box, <ui.Card, <List[T], />, <> and </>.
syn match goxTagDelim "</\?\ze\h[[:alnum:]_.]*\%(\[[^\]]*\]\)\?\%(\_s\|/\|>\)"
syn match goxTagName "\%(</\?\)\@<=\h[[:alnum:]_.]*\ze\%(\[[^\]]*\]\)\?\%(\_s\|/\|>\)"
syn match goxTagDelim "/>\|<>\|</>"

" Attributes (name= before a value) and spread {...expr}.
syn match goxAttr "\%(\s\)\@<=\h[[:alnum:]_-]*\ze=\%(\"\|'\|{\|<\)"
syn match goxSpread "{\s*\.\.\."

hi def link goxTagName  Function
hi def link goxTagDelim Delimiter
hi def link goxAttr     Identifier
hi def link goxSpread   Operator

let b:current_syntax = "gox"
