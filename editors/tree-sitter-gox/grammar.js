/**
 * @file Tree-sitter grammar for .gox: Go with TSX-style XML elements.
 * @license MIT
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

const go = require('tree-sitter-go/grammar');

module.exports = grammar(go, {
  name: 'gox',

  rules: {
    // Elements are operands: they can appear wherever an expression can.
    _expression: ($, previous) => choice(
      previous,
      $.element,
      $.self_closing_element,
      $.fragment,
    ),

    element: $ => seq(
      field('open_tag', $.start_tag),
      repeat($._child),
      field('close_tag', $.end_tag),
    ),

    self_closing_element: $ => seq(
      '<',
      field('name', $.tag_name),
      optional(field('type_arguments', $.type_arguments)),
      repeat($._attribute),
      '/',
      '>',
    ),

    start_tag: $ => seq(
      '<',
      field('name', $.tag_name),
      optional(field('type_arguments', $.type_arguments)),
      repeat($._attribute),
      '>',
    ),

    end_tag: $ => seq(
      '<',
      '/',
      field('name', $.tag_name),
      optional(field('type_arguments', $.type_arguments)),
      '>',
    ),

    fragment: $ => seq('<', '>', repeat($._child), '<', '/', '>'),

    // box, Card, ui.Card
    tag_name: _ => /[_\p{XID_Start}][_\p{XID_Continue}]*(\.[_\p{XID_Start}][_\p{XID_Continue}]*)*/,

    _attribute: $ => choice($.attribute, $.spread_attribute),

    attribute: $ => seq(
      field('name', $.attribute_name),
      optional(seq(
        '=',
        field('value', choice(
          $.attribute_string,
          $.expression_container,
          $.element,
          $.self_closing_element,
        )),
      )),
    ),

    attribute_name: _ => /[_\p{XID_Start}][_\p{XID_Continue}]*(-[_\p{XID_Continue}]+)*/,

    attribute_string: _ => token(choice(/"[^"]*"/, /'[^']*'/)),

    spread_attribute: $ => seq('{', '...', $._expression, '}'),

    expression_container: $ => seq('{', optional($._expression), '}'),

    _child: $ => choice(
      $.element,
      $.self_closing_element,
      $.fragment,
      $.expression_container,
      $.text,
    ),

    text: _ => token(prec(-1, /[^{}<>\s]([^{}<>\n]*[^{}<>\s])?/)),
  },
});
