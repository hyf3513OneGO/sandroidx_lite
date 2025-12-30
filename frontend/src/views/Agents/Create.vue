<script setup>
import { onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import { createAgent } from '../../api/agents'
import { createSandbox } from '../../api/sandboxes'
import { listTemplates } from '../../api/templates'
import FormBuilder from '../../components/FormBuilder/FormBuilder.vue'

const router = useRouter()

const loading = ref(false)
const step = ref(0)
const templateLoading = ref(false)
const templates = ref([])
const selectedTemplateId = ref(null)

const form = reactive({
  useTemplate: false,
  instanceCount: 1, // 实例数量，默认1
  image: '',
  required_env_variables: [],
  envs: [{ key: '', value: '' }],
  mounts: [{ volume: '', container_path: '', read_only: false }],
  setup_commands: [],
  running_variables: [],
  running_commands: [],
  createSandbox: false,
  sandbox: {
    type: '',
    image: '',
    ports: [],
    privileged: false,
    args: [],
    setup_adb_commands: [],
    envs: [{ key: '', value: '' }],
    mounts: [{ volume: '', container_path: '', read_only: false }],
    apks: [],
  },
})

const loadTemplates = async () => {
  if (templates.value.length || templateLoading.value) return
  templateLoading.value = true
  try {
    const { data } = await listTemplates()
    templates.value = data || []
  } catch (err) {
    message.error(err.message || '加载模板失败')
  } finally {
    templateLoading.value = false
  }
}

const resetEnvByTemplate = () => {
  const requiredRows = (form.required_env_variables || []).map((name) => ({
    key: name,
    value: '',
  }))
  form.envs = requiredRows.length ? requiredRows : [{ key: '', value: '' }]
}

const applyTemplate = (tpl) => {
  const content = tpl?.content || {}
  const agent = content.agent || {}
  const sandbox = content.sandbox || {}

  form.image = agent.image || ''
  form.required_env_variables = agent.required_env_variables || []
  form.setup_commands = agent.setup_commands || []
  form.running_variables = agent.running_variables || []
  form.running_commands = agent.running_commands || []
  form.mounts = agent.mounts || [{ volume: '', container_path: '', read_only: false }]

  form.createSandbox = !!content.sandbox
  form.sandbox = {
    type: sandbox.type || '',
    image: sandbox.image || '',
    ports: sandbox.ports || [],
    privileged: sandbox.privileged || false,
    args: sandbox.args || [],
    setup_adb_commands: sandbox.setup_adb_commands || [],
    envs: sandbox.envs
      ? Object.entries(sandbox.envs).map(([key, value]) => ({ key, value }))
      : [{ key: '', value: '' }],
    mounts: sandbox.mounts || [{ volume: '', container_path: '', read_only: false }],
    apks: (sandbox.apks || []).map((apk) => ({
      name: apk.name || '',
      package_name: apk.package_name || '',
      version: apk.version || '',
      url: apk.url || apk.url_str || '',
      type: apk.type || 'remote',
    })),
  }

  resetEnvByTemplate()
}

const handleTemplateSelect = (id) => {
  selectedTemplateId.value = id
  const tpl = templates.value.find((t) => t.id === id)
  if (tpl) {
    applyTemplate(tpl)
  }
}

const toMap = (entries = []) => {
  return entries.reduce((acc, cur) => {
    const key = (cur.key || '').trim()
    if (key) acc[key] = cur.value
    return acc
  }, {})
}

const handleSubmit = async () => {
  loading.value = true
  try {
    const instanceCount = form.instanceCount || 1
    const isBatch = instanceCount > 1
    
    // 1. 如果需要创建 Sandbox，先批量创建
    let sandboxHide = null
    if (form.createSandbox && form.sandbox.image) {
      if (isBatch) {
        sandboxHide = message.loading(`正在创建 ${instanceCount} 个 Sandbox...`, 0)
      } else {
        sandboxHide = message.loading('正在创建 Sandbox...', 0)
      }
      
      const sandboxPayload = {
        type: form.sandbox.type || 'phone/redroid',
        image: form.sandbox.image,
        mounts: form.sandbox.mounts.filter(m => m.container_path),
        ports: form.sandbox.ports || [],
        privileged: form.sandbox.privileged || false,
        args: form.sandbox.args || [],
        setup_adb_commands: form.sandbox.setup_adb_commands || [],
        envs: toMap(form.sandbox.envs),
        apks: (form.sandbox.apks || [])
          .filter((a) => (a?.package_name || '').trim() && (a?.version || '').trim())
          .map((a) => ({
            name: (a.name || '').trim(),
            package_name: (a.package_name || '').trim(),
            version: (a.version || '').trim(),
            url: (a.url || '').trim(),
            type: (a.type || 'remote').trim(),
          })),
      }
      
      try {
        // 批量创建 Sandbox
        const sandboxPromises = []
        for (let i = 0; i < instanceCount; i++) {
          sandboxPromises.push(createSandbox(sandboxPayload))
        }
        await Promise.all(sandboxPromises)
        if (sandboxHide) sandboxHide()
        message.success(`成功创建 ${instanceCount} 个 Sandbox`)
      } catch (err) {
        if (sandboxHide) sandboxHide()
        message.error(`Sandbox 创建失败: ${err.message}`)
        throw new Error(`Sandbox 创建失败: ${err.message}`)
      }
    }
    
    // 2. 批量创建 Agent
    const payload = {
      image: form.image,
      required_env_variables: form.useTemplate ? form.required_env_variables : [],
      mounts: form.mounts,
      setup_commands: form.setup_commands,
      running_variables: form.running_variables,
      running_commands: form.running_commands,
      envs: form.useTemplate ? toMap(form.envs) : {},
    }
    
    let agentHide = null
    if (isBatch) {
      agentHide = message.loading(`正在创建 ${instanceCount} 个 Agent...`, 0)
    } else {
      agentHide = message.loading('正在创建 Agent...', 0)
    }
    
    try {
      const agentPromises = []
      for (let i = 0; i < instanceCount; i++) {
        agentPromises.push(createAgent(payload))
      }
      await Promise.all(agentPromises)
      if (agentHide) agentHide()
      message.success(`成功提交 ${instanceCount} 个 Agent 创建请求`)
    } catch (err) {
      if (agentHide) agentHide()
      throw err
    }
    
    step.value = 0
    // 创建完成后返回 Agent 列表
    router.push('/agents')
  } catch (err) {
    message.error(err.message || '创建失败')
  } finally {
    loading.value = false
  }
}

const nextStep = () => {
  step.value = Math.min(step.value + 1, 2)
}

const prevStep = () => {
  step.value = Math.max(step.value - 1, 0)
}

watch(
  () => form.useTemplate,
  (val) => {
    if (val) {
      loadTemplates()
    } else {
      selectedTemplateId.value = null
      form.required_env_variables = []
    form.envs = [{ key: '', value: '' }]
    }
  },
)

onMounted(() => {
  // 手工创建时保持空白，使用模板时再拉取
})

const addRow = (list, row) => list.push({ ...row })
const removeRow = (list, index) => list.splice(index, 1)
</script>

<template>
  <div class="page-container create-page">
    <div class="page-title">
      <h1>创建 Agent</h1>
      <p class="muted">三步完成：基础配置 → 运行命令 → Sandbox（可选）。</p>
    </div>

    <a-steps :current="step" size="small" class="steps">
      <a-step title="基础配置" />
      <a-step title="Setup & Running" />
      <a-step title="Sandbox 可选" />
    </a-steps>

    <div v-if="step === 0" class="card-grid">
      <a-card title="基础配置" bordered>
        <a-form layout="vertical">
          <a-form-item label="是否从模板创建">
            <a-switch v-model:checked="form.useTemplate" /> 使用模板时需先选择模板，再填写必填环境变量值
          </a-form-item>
          <template v-if="form.useTemplate">
            <a-form-item label="选择模板">
              <a-select
                v-model:value="selectedTemplateId"
                :loading="templateLoading"
                placeholder="请选择模板"
                :options="templates.map((t) => ({ label: t.name, value: t.id }))"
                @change="handleTemplateSelect"
                allow-clear
              />
            </a-form-item>
            <a-form-item label="实例数量">
              <a-input-number
                v-model:value="form.instanceCount"
                :min="1"
                :max="100"
                placeholder="要启动的实例数量"
                style="width: 100%"
              />
              <p class="muted" style="margin-top: 4px; margin-bottom: 0">
                指定要创建的实例数量，将创建相同数量的 Agent{{ form.createSandbox ? ' 和 Sandbox' : '' }}
              </p>
            </a-form-item>
          </template>
          <a-form-item label="镜像" required>
            <a-input
              v-model:value="form.image"
              :disabled="form.useTemplate && !!selectedTemplateId"
              placeholder="hyf3513/agent-ubuntu-22.04:latest"
            />
          </a-form-item>
          <a-form-item label="环境变量">
            <div class="kv-list">
              <div v-for="(item, idx) in form.envs" :key="idx" class="kv-row">
                <a-input
                  v-model:value="item.key"
                  placeholder="KEY"
                  style="width: 40%"
                  allow-clear
                  :disabled="form.useTemplate && form.required_env_variables.includes(item.key)"
                />
                <a-input
                  v-model:value="item.value"
                  placeholder="VALUE"
                  style="width: 50%"
                  allow-clear
                />
                <a-button
                  type="text"
                  danger
                  :disabled="form.envs.length === 1 || (form.useTemplate && form.required_env_variables.includes(item.key))"
                  @click="removeRow(form.envs, idx)"
                >
                  删除
                </a-button>
              </div>
              <a-button type="dashed" block @click="addRow(form.envs, { key: '', value: '' })">
                添加环境变量
              </a-button>
            </div>
            <p class="muted" v-if="form.useTemplate">
              模板必填的环境变量键已锁定，可新增额外键值。
            </p>
          </a-form-item>
        </a-form>
      </a-card>

      <a-card title="挂载配置" bordered>
        <div class="kv-list">
          <div v-for="(m, idx) in form.mounts" :key="idx" class="kv-row">
            <a-input v-model:value="m.volume" placeholder="Volume ID（留空自动创建）" />
            <a-input v-model:value="m.container_path" placeholder="/root/data" />
            <a-switch v-model:checked="m.read_only" size="small" /> 只读
            <a-button
              type="text"
              danger
              :disabled="form.mounts.length === 1"
              @click="removeRow(form.mounts, idx)"
            >
              删除
            </a-button>
          </div>
          <a-button
            type="dashed"
            block
            @click="addRow(form.mounts, { volume: '', container_path: '/root/data', read_only: false })"
          >
            添加挂载
          </a-button>
        </div>
      </a-card>
    </div>

    <div v-else-if="step === 1" class="card-grid">
      <a-card title="Setup 命令（初始化）" bordered>
        <p class="muted">容器启动后自动执行的初始化命令，例如安装依赖、克隆代码等。</p>
        <div class="kv-list">
          <div v-for="(c, idx) in form.setup_commands" :key="`setup-${idx}`" class="kv-row column">
            <a-input v-model:value="c.workdir" placeholder="workdir (可选)" />
            <a-input
              v-model:value="c.run"
              placeholder='git clone https://github.com/xxx/yyy.git'
              allow-clear
            />
            <a-button
              type="text"
              danger
              @click="removeRow(form.setup_commands, idx)"
            >
              删除
            </a-button>
          </div>
          <a-button
            type="dashed"
            block
            @click="addRow(form.setup_commands, { workdir: '', run: '' })"
          >
            添加 Setup 命令
          </a-button>
        </div>
      </a-card>

      <a-card title="Running 变量与命令" bordered>
        <div class="section-subtitle">变量</div>
        <div class="kv-list">
          <div v-for="(v, idx) in form.running_variables" :key="idx" class="kv-row">
            <a-input v-model:value="v.name" placeholder="<sandroidx_prompt_1>" style="width: 30%" />
            <a-input
              v-model:value="v.description"
              placeholder="描述"
              style="width: 40%"
              allow-clear
            />
            <a-select v-model:value="v.type" style="width: 20%">
              <a-select-option value="string">string</a-select-option>
              <a-select-option value="number">number</a-select-option>
              <a-select-option value="boolean">boolean</a-select-option>
            </a-select>
            <a-switch v-model:checked="v.required" /> 必填
            <a-button
              type="text"
              danger
              :disabled="form.running_variables.length === 1"
              @click="removeRow(form.running_variables, idx)"
            >
              删除
            </a-button>
          </div>
          <a-button
            type="dashed"
            block
            @click="
              addRow(form.running_variables, {
                name: '',
                description: '',
                type: 'string',
                required: false,
              })
            "
          >
            添加变量
          </a-button>
        </div>

        <div class="section-subtitle">命令</div>
        <div class="kv-list">
          <div v-for="(c, idx) in form.running_commands" :key="idx" class="kv-row column">
            <a-input v-model:value="c.workdir" placeholder="workdir (可选)" />
            <a-input
              v-model:value="c.run"
              placeholder='python main.py "<sandroidx_prompt_1>"'
              allow-clear
            />
            <a-button
              type="text"
              danger
              :disabled="form.running_commands.length === 1"
              @click="removeRow(form.running_commands, idx)"
            >
              删除
            </a-button>
          </div>
          <a-button
            type="dashed"
            block
            @click="addRow(form.running_commands, { workdir: '', run: '' })"
          >
            添加命令
          </a-button>
        </div>

        <div class="section-subtitle">动态表单预览 & 命令预览</div>
        <FormBuilder
          :variables="form.running_variables"
          :commands="form.running_commands"
          :loading="loading"
          :show-execute="false"
          :enable-required="false"
          :enable-submit="false"
          @submit="handleSubmit"
        />
      </a-card>
    </div>

    <div v-else class="card-grid">
      <a-card title="Sandbox（可选）" bordered>
        <a-switch v-model:checked="form.createSandbox" /> 创建关联 Sandbox
        <div v-if="form.createSandbox" class="sandbox">
          <a-row :gutter="[16, 16]">
            <a-col :xs="24" :md="12">
              <a-form layout="vertical">
                <a-form-item label="类型">
                  <a-input v-model:value="form.sandbox.type" />
                </a-form-item>
                <a-form-item label="镜像">
                  <a-input v-model:value="form.sandbox.image" />
                </a-form-item>
                <a-form-item label="端口">
                  <a-tag v-for="(p, i) in form.sandbox.ports" :key="i">{{ p }}</a-tag>
                </a-form-item>
                <a-form-item>
                  <a-switch v-model:checked="form.sandbox.privileged" /> 特权模式
                </a-form-item>
              </a-form>
            </a-col>
            <a-col :xs="24" :md="12">
              <div class="kv-list">
                <div v-for="(m, idx) in form.sandbox.mounts" :key="idx" class="kv-row">
                  <a-input v-model:value="m.volume" placeholder="Volume ID" />
                  <a-input v-model:value="m.container_path" placeholder="/data" />
                  <a-switch v-model:checked="m.read_only" size="small" /> 只读
                  <a-button
                    type="text"
                    danger
                    :disabled="form.sandbox.mounts.length === 1"
                    @click="removeRow(form.sandbox.mounts, idx)"
                  >
                    删除
                  </a-button>
                </div>
                <a-button
                  type="dashed"
                  block
                  @click="
                    addRow(form.sandbox.mounts, {
                      volume: '',
                      container_path: '/data',
                      read_only: false,
                    })
                  "
                >
                  添加挂载
                </a-button>
              </div>
            </a-col>
          </a-row>

          <a-row :gutter="[16, 16]" style="margin-top: 12px">
            <a-col :xs="24">
              <div class="section-subtitle">APK 安装配置（将在 setup_adb_commands 之前安装）</div>
              <div class="kv-list">
                <div v-for="(apk, idx) in form.sandbox.apks" :key="idx" class="apk-row">
                  <a-row :gutter="8" style="width: 100%">
                    <a-col :xs="24" :md="6">
                      <a-input v-model:value="apk.name" placeholder="名称（可选）" />
                    </a-col>
                    <a-col :xs="24" :md="6">
                      <a-input v-model:value="apk.package_name" placeholder="包名 *" />
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-input v-model:value="apk.version" placeholder="版本 *" />
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-select v-model:value="apk.type" style="width: 100%">
                        <a-select-option value="remote">remote</a-select-option>
                        <a-select-option value="local">local</a-select-option>
                      </a-select>
                    </a-col>
                    <a-col :xs="24" :md="4">
                      <a-input
                        v-model:value="apk.url"
                        :placeholder="apk.type === 'remote' ? 'URL *' : 'URL（local 时为空）'"
                      />
                    </a-col>
                  </a-row>
                  <a-button
                    type="text"
                    danger
                    @click="removeRow(form.sandbox.apks, idx)"
                  >
                    删除
                  </a-button>
                </div>
                <a-button
                  type="dashed"
                  block
                  @click="addRow(form.sandbox.apks, { name: '', package_name: '', version: '', url: '', type: 'remote' })"
                >
                  添加 APK
                </a-button>
              </div>
            </a-col>
          </a-row>
        </div>
      </a-card>
    </div>

    <div class="wizard-actions">
      <a-space>
        <a-button :disabled="step === 0" @click="prevStep">上一步</a-button>
        <a-button v-if="step < 2" type="primary" @click="nextStep">下一步</a-button>
        <a-button v-else type="primary" :loading="loading" @click="handleSubmit">提交创建</a-button>
      </a-space>
    </div>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.create-page {
  padding: @space-xl 0 @space-xxl;
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.page-title h1 {
  margin: 0 0 @space-sm;
  font-size: @font-size-h1;
}

.kv-list {
  display: flex;
  flex-direction: column;
  gap: @space-sm;
}

.kv-row {
  display: flex;
  align-items: center;
  gap: @space-sm;

  &.column {
    flex-direction: column;
    align-items: flex-start;
  }

  @media (max-width: 768px) {
    flex-direction: column;
    align-items: flex-start;
  }
}

.section-subtitle {
  margin: @space-md 0 @space-sm;
  font-weight: @font-weight-medium;
}

.sandbox {
  margin-top: @space-md;
}

.apk-row {
  display: flex;
  align-items: flex-start;
  gap: @space-sm;
  padding: @space-sm;
  border: 1px solid #d9d9d9;
  border-radius: 4px;
  margin-bottom: @space-sm;
}
</style>

