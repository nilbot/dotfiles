# conda and mamba
if test -d "$HOME"/sdk/miniforge3
    set CONDA_BIN "$HOME"/sdk/miniforge3/bin/conda 
else if test -d /opt/homebrew/Caskroom/miniforge/base
    set CONDA_BIN /opt/homebrew/Caskroom/miniforge/base/bin/conda
else if test -d "$HOME"/miniconda3
    set CONDA_BIN "$HOME"/miniconda3/bin/conda
else if test -d "$HOME"/sdk/mambaforge
    set CONDA_BIN "$HOME"/sdk/mambaforge/bin/conda
end
if test -n "$CONDA_BIN"
    eval $CONDA_BIN "shell.fish" "hook" $argv | source
    set -e CONDA_BIN
end