# The install script is made by `mygo build` and published with every release;
# this file only keeps the address of older instructions working.
#
# usage: irm https://github.com/marukolibro/Hop/releases/latest/download/install.ps1 | iex

$ErrorActionPreference = 'Stop'
Invoke-RestMethod -Uri 'https://github.com/marukolibro/Hop/releases/latest/download/install.ps1' | Invoke-Expression
