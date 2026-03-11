package ait

import "fmt"

// RunCompletion prints a shell completion script for the given shell.
func RunCompletion(shell string) error {
	switch shell {
	case "bash":
		fmt.Print(bashCompletionScript)
		return nil
	case "zsh":
		fmt.Print(zshCompletionScript)
		return nil
	default:
		return &CLIError{
			Code:     "usage",
			Message:  fmt.Sprintf("unsupported shell %q (supported: bash, zsh)", shell),
			ExitCode: 64,
		}
	}
}

const bashCompletionScript = `_ait_completions() {
    local cur prev words cword
    # Use _init_completion if available (bash-completion package),
    # otherwise set variables manually for compatibility.
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion || return
    else
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
        words=("${COMP_WORDS[@]}")
        cword=${COMP_CWORD}
    fi

    local commands="init config create show list search status ready update close reopen cancel claim unclaim dep note export flush completion version help"
    local dep_subcmds="add remove list tree"
    local note_subcmds="add list"
    local completion_subcmds="bash zsh"
    local statuses="open in_progress closed cancelled"
    local types="task epic"
    local priorities="P0 P1 P2 P3 P4"

    # Commands that accept an issue ID as first positional arg
    local id_commands="show update close reopen cancel claim unclaim export"

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=($(compgen -W "${commands}" -- "${cur}"))
        return
    fi

    local cmd="${words[1]}"

    case "${cmd}" in
        dep)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=($(compgen -W "${dep_subcmds}" -- "${cur}"))
                return
            fi
            # Issue ID completion for dep subcommands
            if [[ ${cword} -ge 3 && "${cur}" != -* ]]; then
                local ids
                ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                return
            fi
            ;;
        note)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=($(compgen -W "${note_subcmds}" -- "${cur}"))
                return
            fi
            # Issue ID completion for note subcommands
            if [[ ${cword} -eq 3 && "${cur}" != -* ]]; then
                local ids
                ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                return
            fi
            ;;
        completion)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=($(compgen -W "${completion_subcmds}" -- "${cur}"))
            fi
            return
            ;;
        list)
            case "${prev}" in
                --status)  COMPREPLY=($(compgen -W "${statuses}" -- "${cur}")); return ;;
                --type)    COMPREPLY=($(compgen -W "${types}" -- "${cur}")); return ;;
                --priority) COMPREPLY=($(compgen -W "${priorities}" -- "${cur}")); return ;;
                --parent)
                    local ids
                    ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                    COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                    return
                    ;;
            esac
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--all --long --human --tree --status --type --priority --parent" -- "${cur}"))
            fi
            return
            ;;
        create)
            case "${prev}" in
                --type)     COMPREPLY=($(compgen -W "${types}" -- "${cur}")); return ;;
                --priority) COMPREPLY=($(compgen -W "${priorities}" -- "${cur}")); return ;;
                --parent)
                    local ids
                    ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
                    COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
                    return
                    ;;
            esac
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--title --description --type --parent --priority" -- "${cur}"))
            fi
            return
            ;;
        update)
            case "${prev}" in
                --status)   COMPREPLY=($(compgen -W "${statuses}" -- "${cur}")); return ;;
                --priority) COMPREPLY=($(compgen -W "${priorities}" -- "${cur}")); return ;;
            esac
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--title --description --status --priority --parent" -- "${cur}"))
                return
            fi
            ;;&
        ready)
            case "${prev}" in
                --type) COMPREPLY=($(compgen -W "${types}" -- "${cur}")); return ;;
            esac
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--long --type" -- "${cur}"))
                return
            fi
            ;;
        close)
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--cascade" -- "${cur}"))
                return
            fi
            ;;&
        export)
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--output" -- "${cur}"))
                return
            fi
            ;;&
        flush)
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--dry-run" -- "${cur}"))
            fi
            return
            ;;
        init)
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=($(compgen -W "--prefix" -- "${cur}"))
            fi
            return
            ;;
    esac

    # Issue ID completion for commands that take IDs
    for c in ${id_commands}; do
        if [[ "${cmd}" == "${c}" && "${cur}" != -* ]]; then
            local ids
            ids=$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')
            COMPREPLY=($(compgen -W "${ids}" -- "${cur}"))
            return
        fi
    done
}

complete -F _ait_completions ait
`

const zshCompletionScript = `#compdef ait

_ait() {
    local -a commands
    commands=(
        'init:Set project prefix for issue IDs'
        'config:Show project configuration'
        'create:Create a new issue'
        'show:Show issue details and notes'
        'list:List issues'
        'search:Search issues by text'
        'status:Show project summary counts'
        'ready:List unblocked issues'
        'update:Update an issue'
        'close:Close an issue'
        'reopen:Reopen a closed/cancelled issue'
        'cancel:Cancel an issue'
        'claim:Claim an issue for an agent'
        'unclaim:Release a claim'
        'dep:Manage dependencies'
        'note:Manage notes'
        'export:Export Markdown briefing'
        'flush:Purge closed/cancelled issues'
        'completion:Print shell completion script'
        'version:Show version'
        'help:Show help'
    )

    local -a statuses=(open in_progress closed cancelled)
    local -a types=(task epic)
    local -a priorities=(P0 P1 P2 P3 P4)

    _ait_issue_ids() {
        local -a ids
        ids=(${(f)"$(ait list --all 2>/dev/null | grep -o '"id": *"[^"]*"' | sed 's/"id": *"//;s/"//')"})
        compadd -a ids
    }

    if (( CURRENT == 2 )); then
        _describe 'command' commands
        return
    fi

    local cmd="${words[2]}"

    case "${cmd}" in
        dep)
            if (( CURRENT == 3 )); then
                local -a dep_subcmds=(
                    'add:Add a dependency'
                    'remove:Remove a dependency'
                    'list:List dependencies'
                    'tree:Show dependency tree'
                )
                _describe 'subcommand' dep_subcmds
            else
                _ait_issue_ids
            fi
            ;;
        note)
            if (( CURRENT == 3 )); then
                local -a note_subcmds=(
                    'add:Add a note'
                    'list:List notes'
                )
                _describe 'subcommand' note_subcmds
            elif (( CURRENT == 4 )); then
                _ait_issue_ids
            fi
            ;;
        completion)
            if (( CURRENT == 3 )); then
                local -a shells=(bash zsh)
                compadd -a shells
            fi
            ;;
        list)
            _arguments \
                '--all[Include closed and cancelled]' \
                '--long[Full JSON output]' \
                '--human[Human-readable table]' \
                '--tree[Tree view]' \
                '--status[Filter by status]:status:(${statuses})' \
                '--type[Filter by type]:type:(${types})' \
                '--priority[Filter by priority]:priority:(${priorities})' \
                '--parent[Filter by parent]:id:_ait_issue_ids'
            ;;
        create)
            _arguments \
                '--title[Issue title]:title:' \
                '--description[Issue description]:description:' \
                '--type[Issue type]:type:(${types})' \
                '--parent[Parent issue]:id:_ait_issue_ids' \
                '--priority[Priority]:priority:(${priorities})'
            ;;
        update)
            if (( CURRENT == 3 )); then
                _ait_issue_ids
            else
                _arguments \
                    '--title[New title]:title:' \
                    '--description[New description]:description:' \
                    '--status[New status]:status:(${statuses})' \
                    '--priority[New priority]:priority:(${priorities})' \
                    '--parent[New parent]:id:_ait_issue_ids'
            fi
            ;;
        ready)
            _arguments \
                '--long[Full JSON output]' \
                '--type[Filter by type]:type:(${types})'
            ;;
        close)
            if [[ "${words[CURRENT]}" == -* ]]; then
                _arguments '--cascade[Close entire subtree]'
            else
                _ait_issue_ids
            fi
            ;;
        export)
            if [[ "${words[CURRENT]}" == -* ]]; then
                _arguments '--output[Output file]:file:_files'
            else
                _ait_issue_ids
            fi
            ;;
        flush)
            _arguments '--dry-run[Show what would be flushed]'
            ;;
        init)
            _arguments '--prefix[Project prefix]:prefix:'
            ;;
        show|reopen|cancel|unclaim)
            _ait_issue_ids
            ;;
        claim)
            if (( CURRENT == 3 )); then
                _ait_issue_ids
            fi
            ;;
    esac
}

_ait "$@"
`
