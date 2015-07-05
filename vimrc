filetype off                  " required

" set the runtime path to include Vundle and initialize
set rtp+=~/.vim/bundle/Vundle.vim
call vundle#begin()
" alternatively, pass a path where Vundle should install plugins
" call vundle#begin('~/some/path/here')

" let Vundle manage Vundle, required
Plugin 'gmarik/Vundle.vim'
Plugin 'fatih/vim-go'
Plugin 'ZenCoding.vim'
Plugin 'ervandew/supertab'
Plugin 'scrooloose/nerdtree'
Plugin 'scrooloose/nerdcommenter'
Plugin 'majutsushi/tagbar'
Plugin 'ctrlp.vim'
Plugin 'vim-coffee-script'
Plugin 'sudo.vim'
Plugin 'xml.vim'
Plugin 'flazz/vim-colorschemes'
Plugin 'honza/vim-snippets'
Plugin 'tpope/vim-dispatch'
Plugin 'bling/vim-airline'
Plugin 'tpope/vim-fugitive'
Plugin 'Lokaltog/vim-easymotion'
Plugin 'TimothyYe/vim-tips'
Plugin 'Shougo/neocomplete'
Plugin 'Shougo/neosnippet'
Plugin 'Shougo/neosnippet-snippets'
Plugin 'mhinz/vim-startify'
Plugin 'uguu-org/vim-matrix-screensaver'

"
" All of your Plugins must be added before the following line
call vundle#end()            " required
filetype plugin indent on    " required
" To ignore plugin indent changes, instead use:
"filetype plugin on
syntax on
color molokai
"
" Brief help
" :PluginList       - lists configured plugins
" :PluginInstall    - installs plugins; append `!` to update or just :PluginUpdate
" :PluginSearch foo - searches for foo; append `!` to refresh local cache
" :PluginClean      - confirms removal of unused plugins; append `!` to auto-approve removal
"
" see :h vundle for more details or wiki for FAQ
" Put your non-Plugin stuff after this line

"Powerline setting
"Set GUI font type
if has("gui_running")
	set guifont=Source\ Code\ Pro\:h18
endif
let g:airline_powerline_fonts = 1

"improve autocomplete menu color
highlight Pmenu ctermbg=238 gui=bold

"Setup SuperTab
let g:SuperTabRetainCompletionType="context"


set nocompatible              " be iMproved, required
set bsdir=buffer
set encoding=utf-8
set laststatus=2
set number

set relativenumber

set backspace=2
set backspace=indent,eol,start

set ic

set tabstop=2
set cindent shiftwidth=2
set autoindent shiftwidth=2

set foldmethod=indent
set foldlevelstart=99 "no fold at start

"set my leader
let mapleader="\<Space>"
let g:mapleader="\<Space>"

"tabs
map <leader>tn :tabnew<cr>
map <leader>te :tabedit
map <leader>tc :tabclose<cr>
map <leader>tm :tabmove

"  映射NERDTree插件
:map <leader>n :NERDTree<CR>
"let loaded_nerd_tree=1
let NERDTreeQuitOnOpen = 1
let NERDChristmasTree=1
let g:NERDTreeWinSize = 32
map <leader>f :NERDTreeToggle<CR>

" Settings for vim-easymotion
let g:EasyMotion_leader_key = ","

"Settings for TagBar
map <leader>b :TagbarToggle<CR>
let g:tagbar_type_go = {
    \ 'ctagstype' : 'go',
    \ 'kinds' : [
        \ 'p:package',
        \ 'i:imports:1',
        \ 'c:constants',
        \ 'v:variables',
        \ 't:types',
        \ 'n:interfaces',
        \ 'w:fields',
        \ 'e:embedded',
        \ 'm:methods',
        \ 'r:constructor',
        \ 'f:functions'
    \ ],
    \ 'sro' : '.',
    \ 'kind2scope' : {
        \ 't' : 'ctype',
        \ 'n' : 'ntype'
    \ },
    \ 'scope2kind' : {
        \ 'ctype' : 't',
        \ 'ntype' : 'n'
    \ },
    \ 'ctagsbin' : 'gotags',
    \ 'ctagsargs' : '-sort -silent'
\ }

"switch window
:map <leader>w <C-W>w

"set zen coding
 let g:user_zen_settings = {
  \  'php' : {
  \    'extends' : 'html',
  \    'filters' : 'c',
  \  },
  \  'xml' : {
  \    'extends' : 'html',
  \  },
  \  'haml' : {
  \    'extends' : 'html',
  \  },
  \  'erb' : {
  \    'extends' : 'html',
  \  },
  \}
"set CtrlP
let g:ctrlp_map = '<c-p>'
let g:ctrlp_cmd = 'CtrlP'
let g:ctrlp_custom_ignore = '\.git$\|\.hg$\|\.svn$\|.rvm$'
let g:ctrlp_working_path_mode=0
let g:ctrlp_match_window_bottom=1
let g:ctrlp_max_height=15
let g:ctrlp_match_window_reversed=0
let g:ctrlp_mruf_max=500
let g:ctrlp_follow_symlinks=1
"use in  edit
imap <C-A> <C-C><c-p>

set wildignore+=*/tmp/*,*.so,*.swp,*.zip,*.jpg,*.png,*.gif,*.jpeg,.DS_Store  " MacOSX/Linux
let g:ctrlp_custom_ignore = '\.git$\|\.hg$\|\.svn$'

" move lines up or down (command - D)
nmap <D-j> mz:m+<cr>`z
nmap <D-k> mz:m-2<cr>`z
vmap <D-j> :m'>+<cr>`<my`>mzgv`yo`z
vmap <D-k> :m'<-2<cr>`>my`<mzgv`yo`z

" Tab move lines left or right (c-Ctrl,s-Shift)
nmap    <c-tab>     v>
nmap    <s-tab>     v<
vmap    <c-tab>     >gv
vmap    <s-tab>     <gv

" tab navigation like zsh
:nmap <leader>h :tabprevious<CR>
:map <leader>h :tabprevious<CR>
:imap <leader>h <Esc>:tabprevious<CR>i

:nmap <leader>l :tabnext<CR>
:map <leader>l :tabnext<CR>
:imap <leader>l <Esc>:tabnext<CR>i

"markdown hightlight
let g:octopress_rake_executable = '/usr/bin/rake'

"scss,sass
au BufRead,BufNewFile *.scss set filetype=scss
au BufRead,BufNewFile *.sass set filetype=scss

"coffee script
au BufWritePost *.coffee silent CoffeeMake!
au BufWritePost *.coffee :CoffeeCompile watch vert

"let skim use slim syntax
au BufRead,BufNewFile *.skim set filetype=slim

"ctags
set tags+=~/gitdb/rails/tags

"auto completed
" Disable AutoComplPop.
let g:acp_enableAtStartup = 0

" Enable omni completion.
autocmd FileType css setlocal omnifunc=csscomplete#CompleteCSS
autocmd FileType html,markdown setlocal omnifunc=htmlcomplete#CompleteTags
autocmd FileType javascript setlocal omnifunc=javascriptcomplete#CompleteJS
autocmd FileType python setlocal omnifunc=pythoncomplete#Complete
autocmd FileType xml setlocal omnifunc=xmlcomplete#CompleteTags

" Enable heavy omni completion.
if !exists('g:neocomplcache_omni_patterns')
  let g:neocomplcache_omni_patterns = {}
endif
let g:neocomplcache_omni_patterns.ruby = '[^. *\t]\.\w*\|\h\w*::'

" code search
let g:ackprg = 'ag --nogroup --nocolor --column'

" Title
let g:startify_custom_header = [
\ ' ▄▄        ▄  ▄▄▄▄▄▄▄▄▄▄▄  ▄            ▄▄▄▄▄▄▄▄▄▄   ▄▄▄▄▄▄▄▄▄▄▄  ▄▄▄▄▄▄▄▄▄▄▄ ',
\ '▐░░▌      ▐░▌▐░░░░░░░░░░░▌▐░▌          ▐░░░░░░░░░░▌ ▐░░░░░░░░░░░▌▐░░░░░░░░░░░▌',
\ '▐░▌░▌     ▐░▌ ▀▀▀▀█░█▀▀▀▀ ▐░▌          ▐░█▀▀▀▀▀▀▀█░▌▐░█▀▀▀▀▀▀▀█░▌ ▀▀▀▀█░█▀▀▀▀ ',
\ '▐░▌▐░▌    ▐░▌     ▐░▌     ▐░▌          ▐░▌       ▐░▌▐░▌       ▐░▌     ▐░▌     ',
\ '▐░▌ ▐░▌   ▐░▌     ▐░▌     ▐░▌          ▐░█▄▄▄▄▄▄▄█░▌▐░▌       ▐░▌     ▐░▌     ',
\ '▐░▌  ▐░▌  ▐░▌     ▐░▌     ▐░▌          ▐░░░░░░░░░░▌ ▐░▌       ▐░▌     ▐░▌     ',
\ '▐░▌   ▐░▌ ▐░▌     ▐░▌     ▐░▌          ▐░█▀▀▀▀▀▀▀█░▌▐░▌       ▐░▌     ▐░▌     ',
\ '▐░▌    ▐░▌▐░▌     ▐░▌     ▐░▌          ▐░▌       ▐░▌▐░▌       ▐░▌     ▐░▌     ',
\ '▐░▌     ▐░▐░▌ ▄▄▄▄█░█▄▄▄▄ ▐░█▄▄▄▄▄▄▄▄▄ ▐░█▄▄▄▄▄▄▄█░▌▐░█▄▄▄▄▄▄▄█░▌     ▐░▌     ',
\ '▐░▌      ▐░░▌▐░░░░░░░░░░░▌▐░░░░░░░░░░░▌▐░░░░░░░░░░▌ ▐░░░░░░░░░░░▌     ▐░▌     ',
\ ' ▀        ▀▀  ▀▀▀▀▀▀▀▀▀▀▀  ▀▀▀▀▀▀▀▀▀▀▀  ▀▀▀▀▀▀▀▀▀▀   ▀▀▀▀▀▀▀▀▀▀▀       ▀      ',
\]

