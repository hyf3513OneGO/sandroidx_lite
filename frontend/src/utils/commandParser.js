const escapeForShell = (value = '') =>
  String(value)
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')

/**
 * 将 running_commands 中的占位符替换为用户输入的变量值
 * @param {Array<{workdir?: string, run: string}>} commands
 * @param {Record<string, string>} variables 变量字典，key 为占位符名称（如 <sandroidx_prompt_1>）
 * @returns 同结构的命令数组，已替换变量
 */
export function substituteVariables(commands = [], variables = {}) {
  return commands.map((cmd) => {
    let run = cmd.run
    Object.entries(variables).forEach(([key, value]) => {
      if (!key) return
      const escaped = escapeForShell(value)
      const pattern = key.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      run = run.replace(new RegExp(pattern, 'g'), escaped)
    })
    return { ...cmd, run }
  })
}

/**
 * 生成预览字符串，便于在表单中展示最终要执行的命令
 */
export function previewCommands(commands = []) {
  return commands.map((cmd) => (cmd.workdir ? `cd ${cmd.workdir} && ${cmd.run}` : cmd.run))
}

