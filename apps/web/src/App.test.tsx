import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { RecipeBody, TestItem } from '@recipe/contracts';
import App from './App';

const machineURL = '/api/machines';
const catalogURL = '/api/machines/ABMCPT01/recipes';
const firstURL = `${catalogURL}/sample.pgs`;
const secondURL = `${catalogURL}/other.pgs`;
const machine3Catalog = '/api/machines/ABMCPT03/recipes';
function item(overrides: Partial<TestItem> = {}): TestItem {
  return { id: 'L6', order: 1, number: '', name: 'IGSS', alias: 'IGSS1', kind: 'measurement', function: 'TEST_IGSS', lower: '-25', upper: '2', unit: 'nA', time: '30 ms', bias: 'Vgs = -5 V', description: 'Gate leakage', sourceLine: 6, fields: [{ key: 'Flag1', value: '1' }, { key: 'Description', value: '' }], parameters: [{ key: 'Vgs', label: 'Gate voltage', value: '-5', unit: 'V', description: 'Force gate voltage', raw: [] }], ...overrides };
}
function body(overrides: Partial<RecipeBody> = {}): RecipeBody {
  return { machineId: 'ABMCPT01', sourceName: 'sample.pgs', format: 'pgs', encoding: 'GBK', sha256: 'a'.repeat(64), tests: [item(), item({ id: 'L20', order: 2, name: 'VTH', alias: '', lower: '1.8', upper: '5', unit: 'V', description: '', parameters: [], time: '', bias: '' }), item({ id: 'L30', order: 3, name: 'BOX', alias: 'BOX_SELECT', kind: 'control', function: 'TEST_BOX_SELECT_CTRL', lower: '', upper: '', unit: '', description: '', parameters: [] })], metadata: [{ key: 'strDutName', value: 'Example' }, { key: 'strComment', value: '' }], warnings: ['PGS 标志位按原值保留'], ...overrides };
}
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'content-type': 'application/json' } });
}
function routes(overrides: Record<string, unknown | (() => Promise<Response>)> = {}) {
  const data: Record<string, unknown | (() => Promise<Response>)> = {
    [machineURL]: [{ id: 'ABMCPT01', name: 'ABMCPT01', format: 'pgs' }, { id: 'ABMCPT03', name: 'ABMCPT03', format: 'dcex' }],
    [catalogURL]: [{ id: 'sample.pgs', name: 'sample.pgs', format: 'pgs' }, { id: 'other.pgs', name: 'other.pgs', format: 'pgs' }],
    [firstURL]: body(), [secondURL]: body({ sourceName: 'other.pgs', tests: [item({ id: 'other-L10', alias: 'OTHER' })] }),
    [machine3Catalog]: [{ id: 'sample.dcex', name: 'sample.dcex', format: 'dcex' }],
    [`${machine3Catalog}/sample.dcex`]: body({ machineId: 'ABMCPT03', format: 'dcex', sourceName: 'sample.dcex', encoding: 'UTF-8', tests: [item({ id: 'dcex-L5', name: 'KEL_C', alias: 'Kelvin', number: 'T1', unit: '编码未确认', time: '3（Range=6）', bias: '原始 Bias 编码，见详情' })], warnings: ['Unit、Range 与 Bias 编码尚待厂商确认'] }),
    ...overrides,
  };
  const mock = vi.fn(async (input: string) => {
    if (!(input in data)) throw new Error(`unexpected request: ${input}`);
    const result = data[input];
    return typeof result === 'function' ? result() : response(result);
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}
async function loaded() { return screen.findByRole('button', { name: '查看 IGSS1 第1项' }); }

describe('machine Recipe Body reading', () => {
  it('reads machine → catalog → recipe, and shows limits, actual parameters and source position', async () => {
    const fetch = routes(); render(<App />); await loaded();
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([machineURL, catalogURL, firstURL]);
    expect(screen.getByRole('combobox', { name: '机台' })).toHaveValue('ABMCPT01');
    expect(screen.getByRole('combobox', { name: '配方文件' })).toHaveValue('sample.pgs');
    const detail = within(screen.getByLabelText('测试项详情'));
    expect(detail.getByRole('heading', { name: 'IGSS1' })).toBeVisible();
    expect(detail.getByText('30 ms')).toBeVisible();
    expect(detail.getByText('Vgs = -5 V')).toBeVisible();
    expect(detail.getByText('机台函数')).toBeVisible();
    expect(detail.getByText('第 6 行')).toBeVisible();
    expect(detail.getByText('Gate voltage')).toBeVisible();
    expect(detail.getByText('-5 V')).toBeVisible();
    expect(screen.queryByRole('button', { name: '查看 BOX_SELECT 第3项' })).not.toBeInTheDocument();
    expect(screen.getByText('PGS 标志位按原值保留')).toBeVisible();
  });

  it('selects another item and exposes all raw fields without inventing missing content', async () => {
    routes(); const user = userEvent.setup(); render(<App />); await loaded();
    await user.click(screen.getByRole('button', { name: '查看 VTH 第2项' }));
    const detail = within(screen.getByLabelText('测试项详情'));
    expect(detail.getByRole('heading', { name: 'VTH' })).toBeVisible();
    expect(detail.getByText('源文件未提供描述')).toBeVisible();
    expect(detail.queryByText('当前机台参数')).not.toBeInTheDocument();
    await user.click(detail.getByText('查看原始字段'));
    expect(detail.getByText('Flag1')).toBeVisible();
    expect(detail.getAllByText('—').length).toBeGreaterThan(0);
    const row = screen.getByRole('button', { name: '查看 VTH 第2项' }).closest('tr');
    expect(row).toHaveAttribute('aria-selected', 'true');
  });

  it('filters measurements, includes control steps on demand, and clears empty search results', async () => {
    routes(); const user = userEvent.setup(); render(<App />); await loaded();
    await user.click(screen.getByRole('checkbox', { name: '显示控制步骤' }));
    await user.click(screen.getByRole('button', { name: '查看 BOX_SELECT 第3项' }));
    expect(within(screen.getByLabelText('测试项详情')).getByRole('heading', { name: 'BOX_SELECT' })).toBeVisible();
    await user.type(screen.getByRole('searchbox'), ' igss ');
    expect(screen.getByRole('button', { name: '查看 IGSS1 第1项' })).toBeVisible();
    expect(screen.queryByRole('button', { name: '查看 BOX_SELECT 第3项' })).not.toBeInTheDocument();
    await user.clear(screen.getByRole('searchbox'));
    await user.type(screen.getByRole('searchbox'), 'no match');
    expect(screen.getByText('没有匹配的测试项')).toBeVisible();
    expect(screen.getByText('选择测试项查看详情')).toBeVisible();
    await user.click(screen.getByRole('button', { name: '清空搜索' }));
    expect(screen.getByRole('searchbox')).toHaveValue('');
    expect(screen.getByRole('button', { name: '查看 VTH 第2项' })).toBeVisible();
  });

  it('shows source metadata, SHA-256 and a byte-preserving original download URL', async () => {
    routes(); const user = userEvent.setup(); render(<App />); await loaded();
    await user.click(screen.getByRole('button', { name: '来源信息' }));
    expect(screen.getByRole('heading', { name: '源文件与解析信息' })).toBeVisible();
    expect(screen.getByText('a'.repeat(64))).toBeVisible();
    expect(screen.getByText('strDutName')).toBeVisible();
    expect(screen.getByText('Example')).toBeVisible();
    expect(screen.getByRole('link', { name: '下载原始机台文件' })).toHaveAttribute('href', `${firstURL}/source`);
    await user.click(screen.getByRole('button', { name: /测试规格/ }));
    expect(screen.getByRole('button', { name: '查看 IGSS1 第1项' })).toBeVisible();
  });

  it('switches machines and presents unresolved DCEX units explicitly', async () => {
    routes(); const user = userEvent.setup(); render(<App />); await loaded();
    await user.type(screen.getByRole('searchbox'), 'IGSS');
    await user.selectOptions(screen.getByRole('combobox', { name: '机台' }), 'ABMCPT03');
    await screen.findByRole('button', { name: '查看 Kelvin 第1项' });
    expect(screen.getByRole('searchbox')).toHaveValue('');
    expect(screen.queryByText('sample.pgs', { selector: 'strong' })).not.toBeInTheDocument();
    expect(screen.getAllByText('编码未确认').length).toBe(2);
    expect(screen.getByText('T1')).toBeVisible();
    expect(within(screen.getByLabelText('测试项详情')).getByText('Code')).toBeVisible();
    expect(screen.getByText('Unit、Range 与 Bias 编码尚待厂商确认')).toBeVisible();
  });

  it('switches recipe files and resets search, control filter and selected item', async () => {
    routes(); const user = userEvent.setup(); render(<App />); await loaded();
    await user.click(screen.getByRole('checkbox', { name: '显示控制步骤' }));
    await user.type(screen.getByRole('searchbox'), 'VTH');
    await user.selectOptions(screen.getByRole('combobox', { name: '配方文件' }), 'other.pgs');
    await screen.findByRole('button', { name: '查看 OTHER 第1项' });
    expect(screen.getByRole('searchbox')).toHaveValue('');
    expect(screen.getByRole('checkbox')).not.toBeChecked();
    expect(within(screen.getByLabelText('测试项详情')).getByRole('heading', { name: 'OTHER' })).toBeVisible();
  });

  it('shows loading immediately and disables refresh while the first request is pending', async () => {
    let complete!: (response: Response) => void;
    routes({ [machineURL]: () => new Promise((resolve) => { complete = resolve; }) });
    render(<App />);
    expect(screen.getByRole('status')).toHaveTextContent('正在读取并解析');
    expect(screen.getByRole('button', { name: '重新读取' })).toBeDisabled();
    expect(screen.getByRole('combobox', { name: '机台' })).toBeDisabled();
    await act(async () => complete(response([{ id: 'ABMCPT01', name: 'ABMCPT01', format: 'pgs' }])));
    await loaded();
    expect(screen.getByRole('button', { name: '重新读取' })).toBeEnabled();
  });

  it('refreshes the entire chain and never displays stale data during reread', async () => {
    const fetch = routes(); const user = userEvent.setup(); render(<App />); await loaded();
    await user.click(screen.getByRole('button', { name: '重新读取' }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(6));
    await loaded();
  });

  for (const stage of [machineURL, catalogURL, firstURL]) {
    it(`shows an error and retries when ${stage} fails`, async () => {
      let failed = true;
      routes({ [stage]: async () => failed ? response({ error: '机台离线，请重试' }, 502) : response(stage === machineURL ? [{ id: 'ABMCPT01', name: 'ABMCPT01', format: 'pgs' }] : stage === catalogURL ? [{ id: 'sample.pgs', name: 'sample.pgs', format: 'pgs' }] : body()) });
      const user = userEvent.setup(); render(<App />);
      expect(await screen.findByRole('alert')).toHaveTextContent('机台离线，请重试');
      expect(screen.queryByRole('button', { name: '查看 IGSS1 第1项' })).not.toBeInTheDocument();
      failed = false;
      await user.click(screen.getByRole('button', { name: '重试' }));
      await loaded();
    });
  }

  it('handles an empty machine directory without requesting recipes', async () => {
    const fetch = routes({ [machineURL]: [] }); render(<App />);
    expect(await screen.findByText('暂未发现机台')).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('handles an empty recipe directory without requesting a file', async () => {
    const fetch = routes({ [catalogURL]: [] }); render(<App />);
    expect(await screen.findByText('该机台暂无配方文件')).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('combobox', { name: '配方文件' })).toBeDisabled();
  });

  it('handles a recipe containing only controls and retains their missing values', async () => {
    routes({ [firstURL]: body({ tests: [item({ kind: 'control', lower: '', upper: '', unit: '', alias: '', parameters: [{ key: 'BOX', label: '', value: '', unit: '', description: '', raw: [] }] })], warnings: [] }) });
    const user = userEvent.setup(); render(<App />);
    expect(await screen.findByText('没有匹配的测试项')).toBeVisible();
    expect(screen.queryByRole('button', { name: '清空搜索' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('checkbox'));
    await screen.findByRole('button', { name: '查看 IGSS 第1项' });
    expect(within(screen.getByLabelText('测试项详情')).getByText('BOX')).toBeVisible();
  });
});
