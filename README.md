# Elph -- a simple PHP static analysis tool

Elph is a static analysis tool for checking your PHP files.
It performs basic checks. For advanced checks, see [PHPStan](https://phpstan.org/).

## Elphfile

Elph is configured using an Elphfile,
which is located in the root of the PHP project
(usually at the same level as, for example, composer.json).

The format is as follows:

  - The Elphfile is divided into three sections (denoted by brackets: `[SECTION]`):
    *Scan*, *Analyze*, and *Ignore*.
  - Lines beginning with `#` or blank lines are ignored.
  - The *Scan* section includes paths that are parsed.
  - If a line begins with `!`, paths prefixed with that value are ignored.
  - The *Analyze* section includes paths that are analyzed.
  - The *Ignore* section includes patterns of errors to ignore.
  - If a line is in parentheses, the pattern is considered a regular expression;
    otherwise, simple glob matching is used (where `*` matches any characters).

To find out the type of a variable at any given time,
the special comment can be used (recognized by Elph).
To find out the type of an expression, one would type:

    $a = /* expr */;
    #debugType $a

Note: Only a subset of expressions is supported,
mainly function calls or accessing class members.
