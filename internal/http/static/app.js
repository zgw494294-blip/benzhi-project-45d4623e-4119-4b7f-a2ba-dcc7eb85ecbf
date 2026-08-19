const emptyPlanSummary = { totalPlans: 0, activePlans: 0, closedPlans: 0, totalSites: 0, completedSites: 0, exceptionCount: 0, pendingReviewCount: 0 };
const state = { plans: [], selected: null, copySource: null, planSummary: emptyPlanSummary, filters: { date: '', dateFrom: '', dateTo: '', category: '', status: '', severity: '', reviewStatus: '', q: '' }, busy: false };

const $ = (selector) => document.querySelector(selector);
const escapeHTML = (value) => String(value ?? '').replace(/[&<>'"]/g, (character) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[character]));

async function requestJSON(url, options = {}) {
  const response = await fetch(url, { headers: { 'Content-Type': 'application/json', ...(options.headers || {}) }, ...options });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error?.message || '请求未完成');
  return body;
}

function statusLabel(status) { return { active: '进行中', closed: '已关闭' }[status] || status; }
function siteStatusLabel(status) { return { pending: '待巡检', in_progress: '当前点位', completed: '已完成' }[status] || status; }
function severityLabel(severity) { return { normal: '正常', minor: '轻微', major: '较重', critical: '严重' }[severity] || severity; }
function reviewLabel(status) { return { not_required: '无需复核', pending: '待复核', approved: '已通过', rejected: '已退回' }[status] || status; }
function dateLabel(value) { return value ? value.replaceAll('-', '.') : '未设日期'; }
function timeLabel(value) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—'; }
function filterQuery() { return new URLSearchParams(Object.entries(state.filters).filter(([, value]) => value)); }

async function loadPlans() {
  state.filters = { date: $('#date-filter').value, dateFrom: $('#date-from-filter').value, dateTo: $('#date-to-filter').value, category: $('#category-filter').value, status: $('#status-filter').value, severity: $('#severity-filter').value, reviewStatus: $('#review-status-filter').value, q: $('#query-filter').value.trim() };
  const result = await requestJSON(`/api/plans?${filterQuery()}`);
  state.plans = result.plans || [];
  state.planSummary = { ...emptyPlanSummary, ...(result.summary || {}) };
  renderPlanList();
  renderPlanSummary();
  updateMetrics();
  await loadObservations();
  if (state.selected) {
    const stillVisible = state.plans.some((plan) => plan.id === state.selected.id);
    if (stillVisible) await selectPlan(state.selected.id, false);
  }
}

async function loadObservations() {
  const result = await requestJSON(`/api/observations?${filterQuery()}`);
  renderObservations(result.observations || [], result.summary);
}

async function exportObservations() {
  const button = $('#export-observations');
  button.disabled = true;
  try {
    const response = await fetch(`/api/observations/export?${filterQuery()}`);
    if (!response.ok) {
      const body = await response.json().catch(() => ({}));
      throw new Error(body.error?.message || '导出未完成');
    }
    const blob = await response.blob();
    const link = document.createElement('a');
    const url = URL.createObjectURL(blob);
    link.href = url;
    link.download = 'inspection-observations.csv';
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 0);
    showToast('观测明细已导出');
  } catch (error) {
    showToast(error.message, true);
  } finally {
    button.disabled = false;
  }
}

function updateMetrics() {
  $('#active-count').textContent = state.planSummary.activePlans;
  $('#closed-count').textContent = state.planSummary.closedPlans;
  $('#review-count').textContent = state.planSummary.pendingReviewCount;
}

function renderPlanSummary() {
  const values = {
    'total-plans': state.planSummary.totalPlans,
    'active-plans': state.planSummary.activePlans,
    'closed-plans': state.planSummary.closedPlans,
    sites: state.planSummary.totalSites,
    'completed-sites': state.planSummary.completedSites,
    exceptions: state.planSummary.exceptionCount,
    pending: state.planSummary.pendingReviewCount,
  };
  Object.entries(values).forEach(([key, value]) => { const node = $(`#plan-summary-${key}`); if (node) node.textContent = String(value); });
}

function refreshCategories() {
  const categories = [...new Set(state.plans.flatMap((plan) => plan.sites.map((site) => site.category)))].sort();
  const select = $('#category-filter');
  const current = select.value;
  select.innerHTML = '<option value="">全部类别</option>' + categories.map((category) => `<option value="${escapeHTML(category)}">${escapeHTML(category)}</option>`).join('');
  select.value = current;
}

function renderPlanList() {
  refreshCategories();
  $('#plan-count').textContent = `${state.plans.length} 条`;
  const list = $('#plan-list');
  $('#plan-empty').classList.toggle('hidden', state.plans.length !== 0);
  list.innerHTML = state.plans.map((plan) => {
    const exceptions = plan.sites.reduce((count, site) => count + site.observations.filter((observation) => observation.severity !== 'normal').length, 0);
    const completed = plan.sites.filter((site) => site.status === 'completed').length;
    return `<article class="plan-card ${state.selected?.id === plan.id ? 'selected' : ''}" data-plan-id="${escapeHTML(plan.id)}" tabindex="0">
      <div class="plan-card-head"><h3>${escapeHTML(plan.name)}</h3><div class="plan-card-tools"><span class="status-pill status-${escapeHTML(plan.status)}">${statusLabel(plan.status)}</span><button class="button button-outline button-small" data-copy-plan type="button">复制计划</button></div></div>
      <div class="plan-card-meta"><span>${escapeHTML(plan.area)} · ${completed}/${plan.sites.length} 点位</span><span>${dateLabel(plan.scheduledDate)}</span></div>
      ${exceptions ? `<div class="plan-card-meta"><span class="severity-pill severity-major">${exceptions} 项异常</span><span>版本 ${plan.version}</span></div>` : `<div class="plan-card-meta"><span>当前无异常</span><span>版本 ${plan.version}</span></div>`}
    </article>`;
  }).join('');
  list.querySelectorAll('.plan-card').forEach((card) => {
    card.addEventListener('click', () => selectPlan(card.dataset.planId));
    card.addEventListener('keydown', (event) => { if (event.key === 'Enter' || event.key === ' ') selectPlan(card.dataset.planId); });
    card.querySelector('[data-copy-plan]').addEventListener('click', (event) => { event.stopPropagation(); prepareCopyPlan(card.dataset.planId); });
  });
}

async function selectPlan(id, showToast = true) {
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(id)}`);
    state.selected = result.plan;
    renderPlanList();
    renderDetail();
    if (showToast) window.location.hash = 'detail';
  } catch (error) { showToast(error.message, true); }
}

function renderDetail() {
  const plan = state.selected;
  $('#detail-empty').classList.toggle('hidden', Boolean(plan));
  $('#detail-content').classList.toggle('hidden', !plan);
  if (!plan) return;
  const exceptions = plan.sites.reduce((count, site) => count + site.observations.filter((observation) => observation.severity !== 'normal').length, 0);
  const pendingReviews = plan.sites.reduce((count, site) => count + site.observations.filter((observation) => observation.reviewStatus === 'pending').length, 0);
  const nextSite = plan.sites.find((site) => site.status !== 'completed');
  $('#detail-content').innerHTML = `<div class="detail-title-row">
    <div><p class="section-kicker">PLAN / ${escapeHTML(plan.id.slice(-8).toUpperCase())}</p><h2>${escapeHTML(plan.name)}</h2><p class="detail-subtitle">${escapeHTML(plan.area)} · ${dateLabel(plan.scheduledDate)}</p></div>
    <div class="detail-tools"><span class="status-pill status-${escapeHTML(plan.status)}">${statusLabel(plan.status)}</span><button class="button button-outline button-small" id="copy-plan-button" type="button">复制计划</button></div>
  </div>
  <div class="detail-stats"><div class="detail-stat"><span>点位进度</span><strong>${plan.sites.filter((site) => site.status === 'completed').length}/${plan.sites.length}</strong></div><div class="detail-stat"><span>异常记录</span><strong>${exceptions}</strong></div><div class="detail-stat"><span>待复核</span><strong>${pendingReviews}</strong></div></div>
  <div class="sequence-heading"><h3>点位顺序</h3><span>${plan.status === 'closed' ? '流程已归档' : nextSite ? `下一站：${escapeHTML(nextSite.name)}` : '等待关闭'}</span></div>
  <div class="site-list">${plan.sites.map((site) => renderSite(plan, site, nextSite)).join('')}</div>
  ${pendingReviews ? renderReviewQueue(plan) : ''}
  ${plan.status === 'active' ? `<div class="close-plan-row"><p>${nextSite ? '完成全部点位并处理异常后可关闭计划。' : '所有点位已完成，可生成巡检报告。'}</p><button class="button button-primary" id="close-plan" type="button" ${nextSite || pendingReviews ? 'disabled' : ''}>关闭并生成报告</button></div>` : renderReport(plan)}`;
  bindDetailActions();
}

function renderSite(plan, site, nextSite) {
  const observation = site.observations[site.observations.length - 1];
  const isNext = plan.status === 'active' && nextSite?.id === site.id;
  return `<section class="site-row"><div class="site-row-head"><div class="site-leading"><span class="site-number">${String(site.sequence).padStart(2, '0')}</span><div class="site-name"><strong>${escapeHTML(site.name)}</strong><span>${escapeHTML(site.category)} · ${escapeHTML(site.location)}</span></div></div><span class="status-pill status-${escapeHTML(site.status)}">${siteStatusLabel(site.status)}</span></div>
    ${observation ? `<div class="site-observation" data-observation-id="${escapeHTML(observation.id)}"><div class="observation-head"><div><strong>${escapeHTML(observation.kind)}：${escapeHTML(observation.value)} ${escapeHTML(observation.unit)}</strong><small>${escapeHTML(observation.observer)} · ${timeLabel(observation.observedAt)}</small></div><span class="severity-pill severity-${escapeHTML(observation.severity)}">${severityLabel(observation.severity)}</span></div>${observation.note ? `<p class="observation-note">${escapeHTML(observation.note)}</p>` : ''}<div class="observation-head" style="margin-top:10px"><span class="review-pill review-${escapeHTML(observation.reviewStatus)}">${reviewLabel(observation.reviewStatus)}</span>${observation.reviewer ? `<small>复核：${escapeHTML(observation.reviewer)}</small>` : ''}</div>${renderReviewHistory(observation)}${plan.status === 'active' && observation.reviewStatus === 'rejected' ? renderReopenForm(plan, observation) : ''}</div>` : isNext ? renderObservationForm(site, plan.version) : ''}
  </section>`;
}

function renderReviewHistory(observation) {
  const history = observation.reviewHistory || [];
  if (history.length === 0) return '';
  return `<div class="review-history"><div class="history-heading"><strong>复核历史</strong><span>${history.length} 次处理</span></div><ol>${history.map((event) => `<li><div><span class="review-pill review-${escapeHTML(event.event === 'reopened' ? 'pending' : event.event)}">${event.event === 'reopened' ? '重新提交' : reviewLabel(event.event === 'approved' ? 'approved' : 'rejected')}</span><b>${escapeHTML(event.operator)}</b></div><time>${timeLabel(event.at)}</time><p>${escapeHTML(event.note || '未填写意见')}</p></li>`).join('')}</ol></div>`;
}

function renderReopenForm(plan, observation) {
  return `<form class="reopen-form" data-reopen-form data-observation-id="${escapeHTML(observation.id)}"><div class="reopen-heading"><div><strong>整改后重新提交复核</strong><span>保留原处理记录，提交后回到待复核队列</span></div><span class="review-pill review-rejected">已退回</span></div><div class="form-grid two-columns"><label><span>整改人</span><input name="operator" required maxlength="40" placeholder="填写整改人"></label><label><span>整改说明</span><textarea name="note" required maxlength="300" placeholder="说明整改内容和复测情况"></textarea></label></div><div class="form-actions"><button class="button button-primary button-small" type="submit" data-version="${plan.version}">重新提交复核</button></div></form>`;
}

function renderObservationForm(site, version) {
  return `<form class="observation-form" data-observation-form data-site-id="${escapeHTML(site.id)}"><div class="form-grid three-columns"><label><span>观测项目</span><input name="kind" required maxlength="60" placeholder="例：照明状态"></label><label><span>现场数值</span><input name="value" required maxlength="100" placeholder="例：正常 / 3 处"></label><label><span>单位</span><input name="unit" maxlength="20" placeholder="—"></label><label><span>异常等级</span><select name="severity"><option value="normal">正常</option><option value="minor">轻微</option><option value="major">较重</option><option value="critical">严重</option></select></label><label><span>备注</span><textarea name="note" maxlength="300" placeholder="记录现场情况和处置线索"></textarea></label><label><span>记录人</span><input name="observer" required maxlength="40" placeholder="姓名"></label></div><div class="form-actions"><button class="button button-primary button-small" type="submit" data-version="${version}">提交观测</button></div></form>`;
}

function renderReviewQueue(plan) {
  const pending = plan.sites.flatMap((site) => site.observations.filter((observation) => observation.reviewStatus === 'pending').map((observation) => ({ site, observation })));
  return `<div class="review-section"><div class="review-section-head"><div><h3>异常复核队列</h3><span>选择同一计划内的待复核观测</span></div><label class="select-all"><input id="review-select-all" type="checkbox">全选</label></div>
    <form class="batch-review-bar" id="batch-review-form"><div class="batch-review-count"><strong id="selected-review-count">0</strong><span>项已选</span></div><div class="batch-review-fields"><label><span>统一复核人</span><input name="reviewer" required maxlength="40" placeholder="主管姓名"></label><label><span>统一复核意见</span><textarea name="note" maxlength="300" placeholder="填写本批观测的复核结论"></textarea></label></div><div class="form-actions batch-review-actions"><button class="button button-quiet button-small" type="submit" data-batch-review data-decision="rejected" disabled>批量退回</button><button class="button button-primary button-small" type="submit" data-batch-review data-decision="approved" disabled>批量通过</button></div></form>
    <div class="review-list">${pending.map(({ site, observation }) => renderReviewCard(plan, site, observation)).join('')}</div></div>`;
}

function renderReviewCard(plan, site, observation) {
  return `<div class="review-card" data-review-id="${escapeHTML(observation.id)}"><div class="observation-head"><div class="review-card-leading"><label class="review-select"><input data-review-checkbox type="checkbox" value="${escapeHTML(observation.id)}" aria-label="选择 ${escapeHTML(site.name)} 的异常观测"><span>选择</span></label><div><strong>${escapeHTML(site.name)} · ${escapeHTML(observation.kind)}</strong><small>${escapeHTML(observation.value)} ${escapeHTML(observation.unit)} · ${escapeHTML(observation.observer)}</small></div></div><span class="severity-pill severity-${escapeHTML(observation.severity)}">${severityLabel(observation.severity)}</span></div><p>${escapeHTML(observation.note || '未填写备注')}</p><form class="review-form" data-review-form><label><span>复核意见</span><textarea name="note" maxlength="300" required placeholder="填写现场复核结论"></textarea></label><label style="display:block;margin-top:8px"><span>复核人</span><input name="reviewer" required maxlength="40" placeholder="主管姓名"></label><div class="form-actions"><button class="button button-quiet button-small" type="submit" data-decision="rejected" data-version="${plan.version}">退回处理</button><button class="button button-primary button-small" type="submit" data-decision="approved" data-version="${plan.version}">确认通过</button></div></form></div>`;
}

function renderReport(plan) {
  if (!plan.report) return '';
  const severityCounts = plan.report.severityCounts || {};
  const reviewCounts = plan.report.reviewCounts || {};
  return `<div class="report-block"><div class="report-head"><div><p class="section-kicker">FINAL REPORT</p><h3>巡检报告已生成</h3></div><span class="status-pill status-closed">不可变记录</span></div><p>${escapeHTML(plan.report.summary)}</p><div class="report-insights"><div><span class="report-insight-label">异常等级</span><div class="report-counts">${reportCountList(severityCounts, [['normal', '正常', 'severity-normal'], ['minor', '轻微', 'severity-minor'], ['major', '较重', 'severity-major'], ['critical', '严重', 'severity-critical']])}</div></div><div><span class="report-insight-label">复核结果</span><div class="report-counts">${reportCountList(reviewCounts, [['not_required', '无需复核', 'review-not-required'], ['pending', '待复核', 'review-pending'], ['approved', '已通过', 'review-approved'], ['rejected', '已退回', 'review-rejected']])}</div></div></div><p class="report-checksum">SHA-256 · ${escapeHTML(plan.report.checksum)}<br>生成时间 · ${timeLabel(plan.report.generatedAt)}</p></div>`;
}

function reportCountList(counts, entries) {
  return entries.map(([key, label, className]) => `<span class="report-count"><span class="${className}">${label}</span><b>${Number(counts[key]) || 0}</b></span>`).join('');
}

function bindDetailActions() {
  document.querySelectorAll('[data-observation-form]').forEach((form) => form.addEventListener('submit', submitObservation));
  document.querySelectorAll('[data-review-form]').forEach((form) => form.addEventListener('submit', submitReview));
  document.querySelectorAll('[data-reopen-form]').forEach((form) => form.addEventListener('submit', submitReopen));
  document.querySelectorAll('[data-review-checkbox]').forEach((checkbox) => checkbox.addEventListener('change', syncReviewSelection));
  $('#review-select-all')?.addEventListener('change', toggleAllReviews);
  $('#batch-review-form')?.addEventListener('submit', submitBatchReview);
  syncReviewSelection();
  $('#close-plan')?.addEventListener('click', closePlan);
  $('#copy-plan-button')?.addEventListener('click', () => prepareCopyPlan(state.selected.id));
}

function selectedReviewIDs() {
  return [...document.querySelectorAll('[data-review-checkbox]:checked')].map((checkbox) => checkbox.value);
}

function syncReviewSelection() {
  const checkboxes = [...document.querySelectorAll('[data-review-checkbox]')];
  const selected = selectedReviewIDs();
  const selectAll = $('#review-select-all');
  if (selectAll) {
    selectAll.checked = checkboxes.length > 0 && selected.length === checkboxes.length;
    selectAll.indeterminate = selected.length > 0 && selected.length < checkboxes.length;
  }
  $('#selected-review-count')?.replaceChildren(document.createTextNode(String(selected.length)));
  document.querySelectorAll('[data-batch-review]').forEach((button) => { button.disabled = selected.length === 0; });
}

function toggleAllReviews(event) {
  document.querySelectorAll('[data-review-checkbox]').forEach((checkbox) => { checkbox.checked = event.currentTarget.checked; });
  syncReviewSelection();
}

async function submitObservation(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const values = new FormData(form);
  const button = form.querySelector('button[type="submit"]');
  button.disabled = true;
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(state.selected.id)}/observations`, { method: 'POST', body: JSON.stringify({ siteID: form.dataset.siteId, kind: values.get('kind'), value: values.get('value'), unit: values.get('unit'), note: values.get('note'), observer: values.get('observer'), severity: values.get('severity'), idempotencyKey: `web-${Date.now()}-${form.dataset.siteId}`, expectedVersion: Number(button.dataset.version) }) });
    state.selected = result.plan;
    showToast('观测已记录');
    await loadPlans();
  } catch (error) { showToast(error.message, true); button.disabled = false; }
}

async function submitReview(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const button = event.submitter;
  const values = new FormData(form);
  button.disabled = true;
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(state.selected.id)}/observations/${encodeURIComponent(form.closest('[data-review-id]').dataset.reviewId)}/review`, { method: 'POST', body: JSON.stringify({ decision: button.dataset.decision, reviewer: values.get('reviewer'), note: values.get('note'), expectedVersion: Number(button.dataset.version) }) });
    state.selected = result.plan;
    showToast('复核结果已保存');
    await loadPlans();
  } catch (error) { showToast(error.message, true); button.disabled = false; }
}

async function submitReopen(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const button = form.querySelector('button[type="submit"]');
  const values = new FormData(form);
  button.disabled = true;
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(state.selected.id)}/observations/${encodeURIComponent(form.dataset.observationId)}/reopen`, { method: 'POST', body: JSON.stringify({ operator: values.get('operator'), note: values.get('note'), expectedVersion: Number(button.dataset.version) }) });
    state.selected = result.plan;
    showToast('整改已重新提交复核');
    await loadPlans();
  } catch (error) { showToast(error.message, true); button.disabled = false; }
}

async function submitBatchReview(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const button = event.submitter;
  const values = new FormData(form);
  const observationIDs = selectedReviewIDs();
  if (observationIDs.length === 0) {
    showToast('请至少选择一条待复核观测', true);
    return;
  }
  button.disabled = true;
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(state.selected.id)}/observations/review-batch`, { method: 'POST', body: JSON.stringify({ observationIDs, decision: button.dataset.decision, reviewer: values.get('reviewer'), note: values.get('note'), expectedVersion: state.selected.version }) });
    state.selected = result.plan;
    showToast('批量复核结果已保存');
    await loadPlans();
  } catch (error) { showToast(error.message, true); syncReviewSelection(); }
}

async function closePlan() {
  const button = $('#close-plan');
  button.disabled = true;
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(state.selected.id)}/close`, { method: 'POST', body: JSON.stringify({ expectedVersion: state.selected.version }) });
    state.selected = result.plan;
    showToast('计划已关闭，报告已生成');
    await loadPlans();
  } catch (error) { showToast(error.message, true); button.disabled = false; }
}

function showToast(message, error = false) {
  const toast = $('#toast'); toast.textContent = message; toast.classList.toggle('error', error); toast.classList.remove('hidden');
  clearTimeout(showToast.timer); showToast.timer = setTimeout(() => toast.classList.add('hidden'), 3400);
}

function renderObservations(rows, summary) {
  renderObservationSummary(summary);
  const body = $('#observation-list');
  $('#observation-empty').classList.toggle('hidden', rows.length !== 0);
  body.innerHTML = rows.slice().reverse().slice(0, 12).map((row) => `<tr><td><strong>${escapeHTML(row.site.name)}</strong><small>${escapeHTML(row.planName)} · ${escapeHTML(row.site.category)}</small></td><td>${escapeHTML(row.observation.kind)}<small>${escapeHTML(row.observation.value)} ${escapeHTML(row.observation.unit)}</small></td><td><span class="severity-pill severity-${escapeHTML(row.observation.severity)}">${severityLabel(row.observation.severity)}</span></td><td>${escapeHTML(row.observation.observer)}</td><td><span class="review-pill review-${escapeHTML(row.observation.reviewStatus)}">${reviewLabel(row.observation.reviewStatus)}</span></td><td>${timeLabel(row.observation.observedAt)}</td></tr>`).join('');
}

function renderObservationSummary(summary = {}) {
  const severity = summary.severity || {};
  const reviewStatus = summary.reviewStatus || {};
  const counts = {
    total: summary.total,
    normal: severity.normal,
    minor: severity.minor,
    major: severity.major,
    critical: severity.critical,
    notRequired: reviewStatus.not_required,
    pending: reviewStatus.pending,
    approved: reviewStatus.approved,
    rejected: reviewStatus.rejected,
  };
  Object.entries(counts).forEach(([key, value]) => { const node = $(`#summary-${key.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)}`); if (node) node.textContent = String(Number(value) || 0); });
}

function addSiteRow(values = {}, readonly = Boolean(state.copySource)) {
  const container = $('#site-rows');
  const row = document.createElement('div'); row.className = 'site-editor-row';
  const readonlyAttribute = readonly ? ' readonly' : '';
  const disabledAttribute = readonly ? ' disabled' : '';
  row.innerHTML = `<span class="row-number"></span><input name="siteName" required maxlength="80" placeholder="点位名称" value="${escapeHTML(values.name || '')}"${readonlyAttribute}><input name="siteCategory" required maxlength="40" placeholder="设施类别" value="${escapeHTML(values.category || '')}"${readonlyAttribute}><input name="siteLocation" required maxlength="120" placeholder="位置描述" value="${escapeHTML(values.location || '')}"${readonlyAttribute}><button type="button" class="remove-site" aria-label="删除点位"${disabledAttribute}>×</button>`;
  row.querySelector('.remove-site').addEventListener('click', () => { if (container.children.length > 1) { row.remove(); renumberSites(); } });
  container.appendChild(row); renumberSites();
}

function renumberSites() { document.querySelectorAll('.site-editor-row .row-number').forEach((node, index) => { node.textContent = String(index + 1).padStart(2, '0'); }); }

function openPlanDialog(sourcePlan = null) {
  state.copySource = sourcePlan;
  const dialog = $('#plan-dialog'); const form = $('#plan-form'); form.reset(); $('#site-rows').innerHTML = ''; $('#plan-form-error').classList.add('hidden');
  $('#plan-dialog-kicker').textContent = sourcePlan ? 'COPY ROUTE' : 'NEW REGISTER';
  $('#plan-dialog-title').textContent = sourcePlan ? '复制巡检计划' : '新建巡检计划';
  $('#submit-plan').textContent = sourcePlan ? '复制并开始' : '创建并开始';
  $('#add-site').disabled = Boolean(sourcePlan);
  if (sourcePlan) {
    form.elements.name.value = sourcePlan.name;
    form.elements.area.value = sourcePlan.area;
    form.elements.scheduledDate.value = sourcePlan.scheduledDate;
    sourcePlan.sites.forEach((site) => addSiteRow(site, true));
  } else {
    addSiteRow();
    form.elements.scheduledDate.value = new Date().toISOString().slice(0, 10);
  }
  dialog.showModal();
}

async function prepareCopyPlan(id) {
  try {
    const result = await requestJSON(`/api/plans/${encodeURIComponent(id)}`);
    openPlanDialog(result.plan);
  } catch (error) { showToast(error.message, true); }
}

async function submitPlan(event) {
  event.preventDefault();
  const form = event.currentTarget; const errorBox = $('#plan-form-error'); const button = $('#submit-plan'); const values = new FormData(form);
  const rows = [...document.querySelectorAll('.site-editor-row')].map((row) => { const inputs = row.querySelectorAll('input'); return { name: inputs[0].value, category: inputs[1].value, location: inputs[2].value }; });
  button.disabled = true; errorBox.classList.add('hidden');
  try {
    const sourcePlan = state.copySource;
    const url = sourcePlan ? `/api/plans/${encodeURIComponent(sourcePlan.id)}/copy` : '/api/plans';
    const body = sourcePlan ? { name: values.get('name'), area: values.get('area'), scheduledDate: values.get('scheduledDate') } : { name: values.get('name'), area: values.get('area'), scheduledDate: values.get('scheduledDate'), sites: rows };
    const result = await requestJSON(url, { method: 'POST', body: JSON.stringify(body) });
    $('#plan-dialog').close(); state.copySource = null; state.selected = result.plan; showToast(sourcePlan ? '计划已复制' : '计划已创建'); await loadPlans(); await selectPlan(result.plan.id, false); window.location.hash = 'detail';
  } catch (error) { errorBox.textContent = error.message; errorBox.classList.remove('hidden'); } finally { button.disabled = false; }
}

function resetFilters() { $('#query-filter').value = ''; $('#date-filter').value = ''; $('#date-from-filter').value = ''; $('#date-to-filter').value = ''; $('#category-filter').value = ''; $('#status-filter').value = ''; $('#severity-filter').value = ''; $('#review-status-filter').value = ''; loadPlans().catch((error) => showToast(error.message, true)); }

$('#new-plan-button').addEventListener('click', openPlanDialog);
$('#add-site').addEventListener('click', () => addSiteRow());
$('#plan-form').addEventListener('submit', submitPlan);
$('#reset-filters').addEventListener('click', resetFilters);
$('#export-observations').addEventListener('click', exportObservations);
['#query-filter', '#date-filter', '#date-from-filter', '#date-to-filter', '#category-filter', '#status-filter', '#severity-filter', '#review-status-filter'].forEach((selector) => $(selector).addEventListener(selector === '#query-filter' ? 'input' : 'change', () => loadPlans().catch((error) => showToast(error.message, true))));
$('#today-label').textContent = new Date().toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }).replaceAll('/', '.');
loadPlans().catch((error) => showToast(error.message, true));
