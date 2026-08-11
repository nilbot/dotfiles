# >>> mojo >>>
if test -d "$HOME/.modular"
    set -gx MODULAR_HOME $HOME/.modular
    fish_add_path "$MODULAR_HOME/bin"
    fish_add_path "$MODULAR_HOME/pkg/packages.modular.com_mojo/bin"
end
# <<< mojo <<<
