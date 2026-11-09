import * as vscode from 'vscode';

const LANGUAGE_ID = 'gi-feature';
const HEADER = /^\s*(Feature|Scenario Outline|Scenario Template|Scenario|Example):/;
const SAFE_ARG = /^[\w+=:./-]+$/;

const terminals = new Map<string, vscode.Terminal>();

export function activate(context: vscode.ExtensionContext): void {
  context.subscriptions.push(
    vscode.commands.registerCommand('gi.runFile', (uri?: vscode.Uri) => run('test', uri)),
    // gi accepts file paths only (no file:line), so a scenario runs its whole file.
    vscode.commands.registerCommand('gi.runScenario', (uri?: vscode.Uri) => run('test', uri)),
    vscode.commands.registerCommand('gi.loadFile', (uri?: vscode.Uri) => run('load', uri)),
    vscode.languages.registerCodeLensProvider({ language: LANGUAGE_ID }, new FeatureCodeLensProvider()),
    vscode.window.onDidCloseTerminal((closed) => {
      for (const [key, terminal] of terminals) {
        if (terminal === closed) {
          terminals.delete(key);
        }
      }
    }),
  );
}

export function deactivate(): void {
  terminals.clear();
}

class FeatureCodeLensProvider implements vscode.CodeLensProvider {
  provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
    const lenses: vscode.CodeLens[] = [];
    for (let line = 0; line < document.lineCount; line++) {
      const header = HEADER.exec(document.lineAt(line).text);
      if (!header) {
        continue;
      }
      const range = new vscode.Range(line, 0, line, 0);
      const args = [document.uri];
      if (header[1] === 'Feature') {
        lenses.push(
          new vscode.CodeLens(range, { title: 'Run', command: 'gi.runFile', arguments: args, tooltip: 'gi test' }),
          new vscode.CodeLens(range, { title: 'Load', command: 'gi.loadFile', arguments: args, tooltip: 'gi load' }),
        );
      } else {
        lenses.push(
          new vscode.CodeLens(range, {
            title: 'Run',
            command: 'gi.runScenario',
            arguments: args,
            tooltip: 'gi test on the whole file: gi cannot select a single scenario by line',
          }),
        );
      }
    }
    return lenses;
  }
}

async function run(subcommand: 'test' | 'load', uri?: vscode.Uri): Promise<void> {
  const target = uri ?? vscode.window.activeTextEditor?.document.uri;
  if (!target || target.scheme !== 'file') {
    void vscode.window.showErrorMessage('gi: open a .feature file on disk first.');
    return;
  }

  const document = vscode.workspace.textDocuments.find((doc) => doc.uri.toString() === target.toString());
  if (document?.isDirty && !(await document.save())) {
    return;
  }

  const folder = vscode.workspace.getWorkspaceFolder(target);
  const cwd = folder ? folder.uri : vscode.Uri.joinPath(target, '..');
  const file = folder ? vscode.workspace.asRelativePath(target, false) : basename(target);
  const executable = vscode.workspace.getConfiguration('gi', target).get<string>('path')?.trim() || 'gi';

  const terminal = terminalFor(cwd, folder?.name);
  terminal.show(true);
  terminal.sendText(commandLine([executable, subcommand, file]));
}

function terminalFor(cwd: vscode.Uri, label?: string): vscode.Terminal {
  const key = cwd.toString();
  const existing = terminals.get(key);
  if (existing && existing.exitStatus === undefined) {
    return existing;
  }
  const terminal = vscode.window.createTerminal({ name: label ? `gi: ${label}` : 'gi', cwd });
  terminals.set(key, terminal);
  return terminal;
}

function basename(uri: vscode.Uri): string {
  return uri.path.slice(uri.path.lastIndexOf('/') + 1);
}

function commandLine(args: string[]): string {
  const shell = vscode.env.shell.toLowerCase();
  const line = args.map((arg) => quote(arg, shell)).join(' ');
  // PowerShell treats a leading quoted string as a value, not a command.
  return isPowerShell(shell) && !SAFE_ARG.test(args[0]) ? `& ${line}` : line;
}

function quote(arg: string, shell: string): string {
  if (SAFE_ARG.test(arg)) {
    return arg;
  }
  if (/(^|[\\/])cmd(\.exe)?$/.test(shell)) {
    return `"${arg}"`;
  }
  if (isPowerShell(shell)) {
    return `'${arg.replace(/'/g, "''")}'`;
  }
  return `'${arg.replace(/'/g, `'\\''`)}'`;
}

function isPowerShell(shell: string): boolean {
  return /(pwsh|powershell)(\.exe)?$/.test(shell);
}
