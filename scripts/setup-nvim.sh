#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: %s [path/to/tell-lsp]\n' "$0"
}

if [[ $# -eq 1 && ( $1 == -h || $1 == --help ) ]]; then
  usage
  exit 0
fi
if [[ $# -gt 1 ]]; then
  usage >&2
  exit 2
fi

if ! config_dir=$(nvim --headless -u NONE -i NONE -n \
    '+lua if vim.fn.has("nvim-0.11") == 1 then io.write(vim.fn.stdpath("config")) else vim.cmd("cquit") end' \
    +qa); then
  printf 'Neovim 0.11 or newer is required.\n' >&2
  exit 1
fi

server=${1:-}
if [[ -z $server ]]; then
  root=$(cd "$(dirname "$0")/.." && pwd -P)
  for candidate in "$root/build/tellc-macos-arm64/tellc/tell-lsp" "$root/build/tellc/tell-lsp"; do
    if [[ -f $candidate && -x $candidate ]]; then
      server=$candidate
      break
    fi
  done
  if [[ -z $server ]]; then
    server=$(command -v tell-lsp || true)
  fi
fi
if [[ ! -f $server || ! -x $server ]]; then
  printf 'Could not find an executable tell-lsp. Install it or pass its path.\n' >&2
  exit 1
fi
server="$(cd "$(dirname "$server")" && pwd -P)/$(basename "$server")"
if [[ $server == *$'\n'* || $server == *$'\r'* ]]; then
  printf 'The tell-lsp path must fit on one line.\n' >&2
  exit 1
fi

plugin="$config_dir/plugin/tell-lsp.lua"
syntax="$config_dir/syntax/tell.vim"
marker='Managed by TellC setup-nvim.sh'
for file in "$plugin" "$syntax"; do
  if [[ -L $file || ( -e $file && ! -f $file ) ]]; then
    printf 'Refusing to replace %s.\n' "$file" >&2
    exit 1
  fi
  if [[ -f $file && $(head -n 1 "$file") != *"$marker" ]]; then
    printf 'Refusing to overwrite your existing %s.\n' "$file" >&2
    exit 1
  fi
done
mkdir -p "$config_dir/plugin" "$config_dir/syntax"

lua_server=${server//\\/\\\\}
lua_server=${lua_server//\"/\\\"}
{
  printf -- '-- %s\nlocal server = "%s"\n' "$marker" "$lua_server"
  cat <<'LUA'

vim.filetype.add({ extension = { tell = "tell" } })

vim.lsp.config("tell", {
  cmd = { server },
  filetypes = { "tell" },
  root_markers = { ".git" },
  on_attach = function(client, bufnr)
    local completion = client.server_capabilities.completionProvider
    if completion then
      completion.triggerCharacters = vim.split("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", "")
      vim.bo[bufnr].completeopt = "menu,menuone,noselect"
      vim.lsp.completion.enable(true, client.id, bufnr, { autotrigger = true })
    end
  end,
})
vim.lsp.enable("tell")
LUA
} > "$plugin"

{
  printf '" %s\n' "$marker"
  cat <<'VIM'
if exists("b:current_syntax")
  finish
endif

syntax case ignore
syntax keyword tellKeyword let constant be set add remove from object own others self to with out function extern returns as end
syntax keyword tellStatement say show print return give back
syntax keyword tellConditional if else otherwise switch
syntax keyword tellRepeat for each while repeat break continue
syntax keyword tellException try raise
syntax keyword tellType number decimal text bool void list vector int32 int64 float32 float64
syntax keyword tellBoolean true false
syntax keyword tellConstant nothing null nil
syntax keyword tellOperator and or not is equal in plus minus times
syntax match tellOperator /[-+*\/%~&|^<>=!]/
syntax match tellNumber /\<\d\+\%(\.\d\+\)\?\>/
syntax match tellEscape /\\./ contained
syntax region tellString oneline start=/"/ skip=/\\./ end=/"/ contains=tellEscape
syntax region tellString oneline start=/'/ skip=/\\./ end=/'/ contains=tellEscape
syntax match tellComment /#.*$/

highlight default link tellKeyword Keyword
highlight default link tellStatement Statement
highlight default link tellConditional Conditional
highlight default link tellRepeat Repeat
highlight default link tellException Exception
highlight default link tellType Type
highlight default link tellBoolean Boolean
highlight default link tellConstant Constant
highlight default link tellOperator Operator
highlight default link tellNumber Number
highlight default link tellEscape SpecialChar
highlight default link tellString String
highlight default link tellComment Comment

let b:current_syntax = "tell"
VIM
} > "$syntax"

printf 'Installed Tell support in %s. Restart Neovim and open a .tell file.\n' "$config_dir"
