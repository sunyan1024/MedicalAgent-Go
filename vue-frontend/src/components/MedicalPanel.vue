<template>
  <section class="medical-panel">
    <div class="medical-heading">
      <strong>医疗助理</strong><span>症状咨询 · 医学知识检索 · 联网搜索 · 报告解读</span>
      <button class="mobile-tools-toggle" :aria-expanded="toolsOpened" aria-controls="medical-tools"
        @click="toolsOpened = !toolsOpened">{{ toolsOpened ? '收起工具' : '报告与示例' }}{{ report ? ' · ' + statusLabel : '' }}</button>
    </div>
    <div id="medical-tools" :class="['medical-tools', { 'is-open': toolsOpened }]">
      <p>用于辅助理解和就医准备，不能替代医生诊断。请先核对报告识别结果。</p>
      <div class="medical-actions">
        <label class="upload-button">上传报告<input type="file" accept=".pdf,.png,.jpg,.jpeg,.txt" :disabled="busy || parsing" @change="uploadFile" /></label>
        <button :disabled="busy || parsing" @click="showText = !showText">粘贴报告</button>
        <button :disabled="busy || parsing" @click="useExample">合成报告样例</button>
        <button :disabled="busy" @click="$emit('example', '我最近感觉乏力，你需要了解哪些信息？')">症状咨询示例</button>
        <button :disabled="busy" @click="$emit('example', '血常规能检查什么？请引用医学知识库资料。')">医学知识示例</button>
      </div>
      <div v-if="showText" class="paste-box">
        <textarea v-model="text" placeholder="粘贴报告原文，保留数值、单位和参考范围" maxlength="60000" />
        <button :disabled="busy || parsing || !text.trim()" @click="uploadText">提交文字</button>
      </div>
      <div v-if="report" class="report-box">
        <strong>{{ report.name }}</strong> · {{ statusLabel }}
        <p v-if="report.error" class="medical-error">{{ report.error }}</p>
        <details v-if="report.text"><summary>查看报告原文</summary><pre>{{ report.text }}</pre></details>
        <div v-if="!parsing" class="indicator-scroll">
          <table><thead><tr><th>指标</th><th>结果</th><th>单位</th><th>同单位参考范围</th><th>原文位置</th><th></th></tr></thead>
            <tbody><tr v-for="(item, index) in indicators" :key="index">
              <td v-for="key in ['name', 'value', 'unit', 'reference', 'source']" :key="key"><input v-model="item[key]" :disabled="busy" @input="markUnconfirmed" /></td>
              <td><button :disabled="busy" @click="indicators.splice(index, 1); markUnconfirmed()">删除</button></td>
            </tr></tbody>
          </table>
          <button :disabled="busy" @click="addIndicator">添加指标</button>
          <button :disabled="busy || !indicators.length" @click="confirm">确认以上数值和单位</button>
          <button :disabled="busy" @click="clearReport">收起报告</button>
        </div>
      </div>
    </div>
    <MedicalRunPanel :session-id="sessionId" :events="events" :busy="busy" :question="question" :request-status="requestStatus" />
  </section>
</template>
<script>
import { ref, computed, watch, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import MedicalRunPanel from './MedicalRunPanel.vue'
import api, { getApiErrorMessage } from '../utils/api'
export default {
  name: 'MedicalPanel',
  components: { MedicalRunPanel },
  props: { sessionId: String, events: { type: Array, default: () => [] }, busy: Boolean, question: String, requestStatus: String },
  emits: ['report', 'ready', 'example'],
  setup(props, { emit }) {
    const toolsOpened = ref(false)
    const report = ref(null), indicators = ref([]), text = ref(''), showText = ref(false), parsing = ref(false)
    let timer, disposed = false, pollGeneration = 0
    const statusLabel = computed(() => ({ parsing: '正在识别', ready: '待确认', confirmed: '已确认', failed: '请手动核对' }[report.value?.status] || ''))
    const markUnconfirmed = () => { if (report.value) report.value.status = 'ready'; emit('ready', false); emit('report', '') }
    const apply = r => { report.value = r; indicators.value = JSON.parse(r.indicatorsJson || '[]'); parsing.value = r.status === 'parsing'; emit('ready', r.status === 'confirmed'); emit('report', r.status === 'confirmed' ? r.id : '') }
    const poll = async (id, generation) => {
      try {
        const { data } = await api.get(`/medical/reports/${id}`)
        if (disposed || generation !== pollGeneration) return
        apply(data.report)
        if (parsing.value) timer = setTimeout(() => poll(id, generation), 1800)
      } catch (e) { if (disposed || generation !== pollGeneration) return; parsing.value = false; ElMessage.error(getApiErrorMessage(e, '报告读取失败')) }
    }
    const upload = async form => {
      clearTimeout(timer); const generation = ++pollGeneration
      parsing.value = true; emit('ready', false); emit('report', '')
      try {
        const { data } = await api.post('/medical/reports', form)
        if (disposed || generation !== pollGeneration) return
        apply(data.report); poll(data.report.id, generation)
      } catch (e) { parsing.value = false; emit('ready', !report.value || report.value.status === 'confirmed'); ElMessage.error(getApiErrorMessage(e, '上传失败')) }
    }
    const uploadFile = event => { const file = event.target.files[0]; if (!file) return; const form = new FormData(); form.append('file', file); upload(form); event.target.value = '' }
    const uploadText = () => { const form = new FormData(); form.append('text', text.value); upload(form) }
    const useExample = () => { text.value = '合成演示报告（非真实患者）\n白细胞 11.2 10^9/L 参考范围 3.5-9.5\n血红蛋白 125 g/L 参考范围 115-150\n血小板 220 10^9/L 参考范围 125-350\n葡萄糖 6.3 mmol/L 参考范围 3.9-6.1\n以上数值和范围仅为演示输入，不作为通用医学参考范围。'; showText.value = true }
    const addIndicator = () => { indicators.value.push({ name: '', value: '', unit: '', reference: '', source: '' }); markUnconfirmed() }
    const confirm = async () => { try { const { data } = await api.patch(`/medical/reports/${report.value.id}`, { indicators: indicators.value }); apply(data.report); ElMessage.success('报告已确认，可以开始提问') } catch (e) { ElMessage.error(getApiErrorMessage(e, '确认失败')) } }
    const clearReport = () => { clearTimeout(timer); ++pollGeneration; report.value = null; indicators.value = []; parsing.value = false; emit('report', ''); emit('ready', true) }
    watch(() => props.sessionId, async id => {
      emit('report', ''); emit('ready', true)
      if (!id || id === 'temp') { clearReport(); return }
      try {
        const { data } = await api.get('/medical/runs', { params: { sessionId: id } })
        if (disposed || id !== props.sessionId) return
        const last = (data.runs || [])[0]
        if (last?.reportId) {
          const res = await api.get(`/medical/reports/${last.reportId}`)
          if (!disposed && id === props.sessionId) apply(res.data.report)
        } else if (report.value?.sessionId && report.value.sessionId !== id) clearReport()
      } catch (e) { ElMessage.error(getApiErrorMessage(e, '运行历史读取失败')) }
    }, { immediate: true })
    onUnmounted(() => { disposed = true; ++pollGeneration; clearTimeout(timer) })
    return { toolsOpened, report, indicators, text, showText, parsing, statusLabel, markUnconfirmed, uploadFile, uploadText, useExample, addIndicator, confirm, clearReport }
  }
}
</script>
<style scoped>
.medical-panel{padding:16px 20px;background:var(--app-surface,#f6faf9);border-bottom:1px solid #d9e5e1;max-height:48vh;overflow:auto;font-size:13px;color:var(--app-text,#253b34)}
.medical-panel{flex-shrink:0;min-height:0}
.mobile-tools-toggle{display:none}
.medical-heading{display:flex;gap:16px;align-items:center}.medical-heading strong{font-size:18px}.medical-heading span,p{color:var(--app-text-muted,#567066)}
.medical-actions{display:flex;gap:8px;flex-wrap:wrap}button,.upload-button{border:1px solid #aacdc0;border-radius:6px;background:var(--app-surface,#fff);color:inherit;padding:7px 10px;cursor:pointer;font-size:12px}.upload-button input{display:none}button:disabled{opacity:.5;cursor:wait}
textarea{box-sizing:border-box;width:100%;min-height:90px;margin-top:10px}.report-box{margin-top:12px}.indicator-scroll{overflow:auto}table{width:100%;margin:10px 0}td input{width:100%;min-width:65px;box-sizing:border-box}th{text-align:left;white-space:nowrap}.medical-error{color:#b84334}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--app-bg,#fff);padding:12px;max-height:220px;overflow:auto}summary{cursor:pointer;padding:5px}
@media (max-width:820px), (max-height:500px) and (pointer:coarse){
  .medical-panel{padding:8px 10px;max-height:var(--chat-panel-max-height,38%);overscroll-behavior-y:contain}
  .medical-heading{justify-content:space-between;gap:8px}
  .medical-heading strong{font-size:15px;white-space:nowrap}
  .medical-heading span{display:none}
  .mobile-tools-toggle{display:block}
  .medical-tools{display:none;margin-top:8px}
  .medical-tools.is-open{display:block}
  .medical-tools p{margin-bottom:8px}
  textarea,td input{font-size:16px}
}
</style>
