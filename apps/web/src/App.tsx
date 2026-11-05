import { useState } from 'react';
import type { Machine, RecipeFile, RecipeBody, TestItem } from '@recipe/contracts';
import { Button } from '@recipe/ui';
import { useRemote } from './api';

const value = (text: string) => text || '—';
const ICONS = {
  'arrow-right': 'icon-[lucide--arrow-right]',
  'circle-alert': 'icon-[lucide--circle-alert]',
  download: 'icon-[lucide--download]',
  'file-code-2': 'icon-[lucide--file-code-2]',
  'file-text': 'icon-[lucide--file-text]',
  info: 'icon-[lucide--info]',
  'list-ordered': 'icon-[lucide--list-ordered]',
  'loader-circle': 'icon-[lucide--loader-circle]',
  'refresh-cw': 'icon-[lucide--refresh-cw]',
  'scan-line': 'icon-[lucide--scan-line]',
  search: 'icon-[lucide--search]',
};

const icon = (glyph: keyof typeof ICONS) => <span aria-hidden="true" className={`icon ${ICONS[glyph]}`} />;

function TestDetail({ item, format }: { item: TestItem; format: string }) {
  return <aside className="detail" aria-label="测试项详情">
    <div className="eyebrow">ITEM INSPECTOR <span>#{String(item.order).padStart(2, '0')}</span></div>
    <h2>{item.alias || item.name}</h2>
    <div className="detail-type">{item.name} <span className="pill">{item.kind === 'control' ? '控制步骤' : '测试项'}</span></div>
    <p className="description">{item.description || '源文件未提供描述'}</p>
    <div className="limit-card"><div>原始规格范围 <span>{value(item.unit)}</span></div><div className="limits"><strong>{value(item.lower)}</strong><span>—</span><strong>{value(item.upper)}</strong></div><div className="range-track"/><div className="limit-labels"><span>LOWER LIMIT</span><span>UPPER LIMIT</span></div></div>
    <dl className="facts"><div><dt>时间</dt><dd>{value(item.time)}</dd></div><div><dt>偏置 / 激励</dt><dd>{value(item.bias)}</dd></div><div><dt>{format === 'pgs' ? '机台函数' : 'Code'}</dt><dd>{item.function}</dd></div><div><dt>来源位置</dt><dd>第 {item.sourceLine} 行</dd></div></dl>
    {item.parameters.length > 0 && <section className="parameter-section"><h3>当前机台参数 <span>{item.parameters.length}</span></h3><div className="parameter-list">{item.parameters.map((parameter, index) => <div key={`${parameter.key}-${index}`} title={parameter.description}><span>{parameter.label || parameter.key}</span><strong>{value(parameter.value)} {parameter.unit}</strong></div>)}</div></section>}
    <details className="raw-fields"><summary>查看原始字段 <span>{item.fields.length}</span></summary><dl>{item.fields.map((field) => <div key={field.key}><dt>{field.key}</dt><dd>{value(field.value)}</dd></div>)}</dl></details>
  </aside>;
}

export default function App() {
  const [revision, setRevision] = useState(0);
  const [requestedMachine, setRequestedMachine] = useState('');
  const [requestedRecipe, setRequestedRecipe] = useState('');
  const [selectedId, setSelectedId] = useState('');
  const [query, setQuery] = useState('');
  const [showControls, setShowControls] = useState(false);
  const [tab, setTab] = useState<'tests' | 'source'>('tests');
  const machines = useRemote<Machine[]>('/api/machines', revision);
  const machineId = machines.data?.find((machine) => machine.id === requestedMachine)?.id ?? machines.data?.[0]?.id;
  const base = machineId ? `/api/machines/${encodeURIComponent(machineId)}/recipes` : undefined;
  const catalog = useRemote<RecipeFile[]>(base, revision);
  const recipeId = catalog.data?.find((recipe) => recipe.id === requestedRecipe)?.id ?? catalog.data?.[0]?.id;
  const detailURL = base && recipeId ? `${base}/${encodeURIComponent(recipeId)}` : undefined;
  const recipe = useRemote<RecipeBody>(detailURL, revision);
  const error = machines.error || catalog.error || recipe.error;
  const loading = machines.loading || catalog.loading || recipe.loading;
  const body = recipe.data;
  const visible = body?.tests.filter((item) => (showControls || item.kind !== 'control') && `${item.name} ${item.alias} ${item.number} ${item.function}`.toLowerCase().includes(query.trim().toLowerCase())) ?? [];
  const selected = visible.find((item) => item.id === selectedId) ?? visible[0];
  const measurements = body?.tests.filter((item) => item.kind === 'measurement').length ?? 0;
  function resetView() { setQuery(''); setSelectedId(''); setShowControls(false); }

  return <div className="app-shell">
    <header className="topbar"><a className="brand" href="/" aria-label="Recipe Body 首页"><span className="brand-mark">{icon('scan-line')}</span><span>Recipe<span className="brand-light"> Body</span></span></a><span className="top-divider"/><span className="workspace-label">配方阅读工作台</span><div className="top-right"><span className="status-dot"/>只读 · 机台数据</div></header>
    <main>
      <div className="breadcrumb">WORKSPACE <span>/</span> CHIP PROBE <span>/</span> RECIPE BODY</div>
      <section className="page-heading"><div><div className="eyebrow">MACHINE RECIPE / 机台配方</div><h1>让机台数据，清晰可读<span>。</span></h1><p>浏览原始测试规格与参数，保留每一项数据的来源。</p></div><Button variant="outline" onClick={() => setRevision((count) => count + 1)} disabled={loading}>{icon('refresh-cw')}重新读取</Button></section>
      <section className="selection-bar" aria-label="机台与配方选择"><div className="station-badge">CP</div><label><span>机台 STATION</span><select aria-label="机台" value={machineId ?? ''} disabled={!machines.data?.length} onChange={(event) => { setRequestedMachine(event.target.value); setRequestedRecipe(''); resetView(); }}><option value="" disabled>请选择机台</option>{machines.data?.map((machine) => <option key={machine.id} value={machine.id}>{machine.name} · {machine.format.toUpperCase()}</option>)}</select></label><div className="selection-divider"/><label className="recipe-select"><span>配方 RECIPE FILE</span><select aria-label="配方文件" value={recipeId ?? ''} disabled={!catalog.data?.length} onChange={(event) => { setRequestedRecipe(event.target.value); resetView(); }}><option value="" disabled>请选择配方文件</option>{catalog.data?.map((file) => <option key={file.id} value={file.id}>{file.name}</option>)}</select></label><div className="source-badge">{body ? `${body.format.toUpperCase()} · ${body.encoding}` : '等待源文件'}</div></section>
      {error ? <div role="alert" className="message error">{icon('circle-alert')}<div><strong>读取失败</strong><p>{error}</p><Button variant="outline" size="sm" onClick={() => setRevision((count) => count + 1)}>重试</Button></div></div> : loading ? <div role="status" className="message loading">{icon('loader-circle')}正在读取并解析机台配方…</div> : !machines.data?.length ? <div className="message" role="status">暂未发现机台</div> : !catalog.data?.length ? <div className="message" role="status">该机台暂无配方文件</div> : body && <>
        <div className="summary-strip"><span>{icon('file-code-2')}<strong>{body.sourceName}</strong></span><span><b>{measurements}</b> 测试项</span><span><b>{body.tests.length - measurements}</b> 控制步骤</span><span className="source-label">源文件已解析 · 顺序保留</span></div>
        {body.warnings.map((warning) => <div className="notice" key={warning}>{icon('info')}<span>{warning}</span></div>)}
        <div className="workbench"><nav className="tabs" aria-label="配方分区"><button aria-current={tab === 'tests' ? 'page' : undefined} onClick={() => setTab('tests')}>{icon('list-ordered')}测试规格 <span>{body.tests.length}</span></button><button aria-current={tab === 'source' ? 'page' : undefined} onClick={() => setTab('source')}>{icon('file-text')}来源信息</button><span>RECIPE BODY READER</span></nav>
          {tab === 'source' ? <section className="source-panel"><h2>源文件与解析信息</h2><dl className="source-facts"><div><dt>机台</dt><dd>{body.machineId}</dd></div><div><dt>原始文件</dt><dd>{body.sourceName}</dd></div><div><dt>编码</dt><dd>{body.encoding}</dd></div><div><dt>SHA-256</dt><dd className="digest">{body.sha256}</dd></div></dl><Button asChild variant="outline"><a href={`${detailURL}/source`} download>{icon('download')}下载原始机台文件</a></Button><h3>源文件元数据与其他分区</h3><table className="metadata-table"><thead><tr><th>字段</th><th>原始值</th></tr></thead><tbody>{body.metadata.map((field, index) => <tr key={`${field.key}-${index}`}><td>{field.key}</td><td>{value(field.value)}</td></tr>)}</tbody></table></section> : <div className="workbench-grid"><section className="test-panel" aria-label="测试规格列表"><div className="table-toolbar"><h2>测试规格 <span>{visible.length} 项</span></h2><label className="control-toggle"><input type="checkbox" checked={showControls} onChange={(event) => setShowControls(event.target.checked)}/>显示控制步骤</label><label className="search-box">{icon('search')}<input type="search" aria-label="搜索测试项" placeholder="搜索名称、别名、编号…" value={query} onChange={(event) => setQuery(event.target.value)}/></label></div><div className="table-scroll"><table className="test-table"><thead><tr><th>#</th><th>测试项 / 别名</th><th>下限</th><th>上限</th><th>单位</th><th>时间</th><th>偏置 / 激励</th></tr></thead><tbody>{visible.map((item) => <tr key={item.id} aria-selected={item.id === selected?.id} onClick={() => setSelectedId(item.id)}><td className="row-number">{item.number || String(item.order).padStart(2, '0')}</td><td><button className="item-link" aria-label={`查看 ${item.alias || item.name} 第${item.order}项`} onClick={() => setSelectedId(item.id)}>{item.alias || item.name}</button><div className="item-name">{item.kind === 'control' ? '控制 · ' : ''}{item.name}</div></td><td className="numeric">{value(item.lower)}</td><td className="numeric">{value(item.upper)}</td><td className="unit-cell">{value(item.unit)}</td><td>{value(item.time)}</td><td className="bias-cell">{value(item.bias)}</td></tr>)}</tbody></table></div>{visible.length === 0 && <div className="empty-state" role="status">没有匹配的测试项{query && <Button variant="ghost" size="sm" onClick={() => setQuery('')}>清空搜索</Button>}</div>}<footer className="table-footer"><span>显示 {visible.length} / {body.tests.length} 项 · 按源文件顺序</span><span>点击测试项查看完整机台字段 {icon('arrow-right')}</span></footer></section>{selected ? <TestDetail key={selected.id} item={selected} format={body.format}/> : <aside className="detail empty-detail">选择测试项查看详情</aside>}</div>}
        </div>
      </>}
      <footer className="page-footer"><span>RECIPE BODY <span>·</span> MACHINE DATA READER</span><span>原始值可追溯，未知编码不推断</span></footer>
    </main>
  </div>;
}
