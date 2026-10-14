import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import eslintConfigPrettier from 'eslint-config-prettier'

// A class that sets what inputClass sets: height, width, background, padding
// or text colour. Variants such as `sm:w-56` are fine, they win by media query.
const inputClassClash =
  '/(^|\\s)([hw]|bg|p[xlr]?|text(?!-(\\[|xs|sm|base|lg|[0-9]?xl|left|right|center|justify|start|end)))-/'

export default tseslint.config(
  { ignores: ['dist'] },
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      ...tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
      eslintConfigPrettier,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
  },
  // Layer boundary: lib, api and i18n are leaves, components/ sits above them.
  {
    files: ['src/lib/**/*.{ts,tsx}', 'src/api/**/*.{ts,tsx}', 'src/i18n/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          patterns: [
            {
              group: ['**/components/**'],
              message: 'lib, api and i18n must not import components/',
            },
          ],
        },
      ],
    },
  },
  // Components and pages read and write the server through the hooks in api/
  // (GLG-1164). No other block sets no-restricted-imports for these files.
  {
    files: ['src/components/**/*.{ts,tsx}', 'src/pages/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: '@tanstack/react-query',
              importNames: [
                'useQuery',
                'useQueries',
                'useInfiniteQuery',
                'useMutation',
                'useQueryClient',
              ],
              message: 'wrap it in a hook in src/api/',
            },
          ],
          patterns: [
            {
              group: ['**/api/*'],
              importNamePattern: '^(create|update|delete|dismiss|save|run|set|test)[A-Z]',
              message: 'call it through a hook in src/api/',
            },
          ],
        },
      ],
    },
  },
  // Remembered choices go through useStoredChoice or createStore, not raw localStorage.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: [
      'src/**/*.test.{ts,tsx}',
      'src/lib/createStore.ts',
      'src/lib/useStoredChoice.ts',
      'src/lib/useReturnSettings.ts',
    ],
    rules: {
      'no-restricted-globals': [
        'error',
        { name: 'localStorage', message: 'use lib/useStoredChoice or lib/createStore' },
      ],
    },
  },
  // Portfolio-scoped queries are invalidated through the helpers in api/transactions,
  // and Intl formatters are built once, not per call (GLG-1170).
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: [
      'src/**/*.test.{ts,tsx}',
      'src/api/transactions.ts',
      'src/api/portfolios.ts',
      'src/api/auth.ts',
    ],
    rules: {
      'no-restricted-syntax': [
        'error',
        // One radio group control, with the roving tab stop (GLG-1248).
        {
          selector: "JSXAttribute[name.name='role'][value.value='radiogroup']",
          message: 'use ui/Segmented',
        },
        // One combobox, on Base UI with its positioning (GLG-1285).
        {
          selector: "JSXAttribute[name.name='role'][value.value='combobox']",
          message: 'use Base UI Autocomplete',
        },
        {
          selector:
            'CallExpression[callee.property.name="invalidateQueries"]:has(Identifier[name=/^(portfolios|summary|history|transactions|expectedDividends|dividendTimeline)QueryKey$/])',
          message: 'use invalidatePortfolio / invalidatePortfolios from api/transactions',
        },
        {
          selector:
            'CallExpression[callee.property.name="invalidateQueries"]:has(ArrayExpression > Literal[value="portfolios"])',
          message: 'use invalidatePortfolio / invalidatePortfolios from api/transactions',
        },
        {
          // `Intl.DateTimeFormat()` without `new` builds one too (GLG-1175).
          selector: ":function :matches(NewExpression, CallExpression)[callee.object.name='Intl']",
          message: 'build Intl formatters once at module scope or cache them',
        },
        {
          selector:
            "JSXOpeningElement[name.name='ul'] > JSXAttribute[name.name='className'][value.value=/(^|\\s)md:hidden(\\s|$)/]",
          message: 'render one layout with useIsDesktop',
        },
        // Focus recovery after a control leaves the page (GLG-1163).
        {
          selector: "BinaryExpression[right.object.name='document'][right.property.name='body']",
          message: 'use lib/useFocusLater',
        },
        // One sortable header, with one aria-sort convention and arrow (GLG-1286).
        { selector: "JSXAttribute[name.name='aria-sort']", message: 'use ui/SortHeader' },
        // One chart frame: error, faded stale chart, skeleton (GLG-1342).
        { selector: "JSXAttribute[name.name='aria-busy']", message: 'use ui/ChartFrame' },
        // One compact table, with its header type, padding and row height (GLG-1289).
        {
          selector: "JSXOpeningElement[name.name='table']:not(:has(Identifier[name='TABLE']))",
          message: 'use TABLE/TH/TD from ui/table',
        },
        // One modal shell, with the nested-cancel guard (GLG-1165).
        { selector: "JSXOpeningElement[name.name='dialog']", message: 'use ui/Modal' },
        // One exact decimal add and subtract (GLG-1288).
        {
          selector:
            "BinaryExpression[operator=/^[-+]$/][left.callee.name='decimalUnits'][right.callee.name='decimalUnits']",
          message: 'use addDecimal / subDecimal from lib/decimal',
        },
        // One sheet panel inside it (GLG-1283). `.` stands for the slash,
        // which an esquery regex cannot hold.
        {
          selector:
            ':matches(Literal[value=/md:-translate-y-1.2/], TemplateElement[value.raw=/md:-translate-y-1.2/])',
          message: 'use sheetPanel from ui/sheetPanel',
        },
        // A control busy with its request keeps focus: one pending prop (GLG-1245).
        {
          selector: "JSXAttribute[name.name='aria-disabled']",
          message: 'use the pending prop of ui/Button or ui/Checkbox',
        },
        // One share format (GLG-1250).
        {
          selector:
            "ObjectExpression:has(> Property[key.name='style'][value.value='percent']):has(> Property[key.name='minimumFractionDigits'][value.value=1])",
          message: 'use SHARE_FORMAT from lib/allocation',
        },
        // A field's height, width, background, text colour and padding come
        // from inputClass, not from editing or appending to its string: two
        // classes for one property conflict (GLG-1284, GLG-1295). Text size
        // and alignment classes are fine.
        {
          selector:
            "CallExpression[callee.property.name='replace']:matches([callee.object.name='inputClass'], [callee.object.callee.name='inputClass'])",
          message: 'pass a size to inputClass',
        },
        {
          selector: `TemplateLiteral:has(> :matches(Identifier[name='inputClass'], CallExpression[callee.name='inputClass'])) > TemplateElement[value.raw=${inputClassClash}]`,
          message: 'pass an option to inputClass',
        },
        // Select appends its className to inputClass, so the same holds for
        // it; a plain string keeps that className readable here (GLG-1298).
        {
          selector: `JSXOpeningElement[name.name='Select'] > JSXAttribute[name.name='className'] > Literal[value=${inputClassClash}]`,
          message: 'pass an option to inputClass in Select',
        },
        {
          selector:
            "JSXOpeningElement[name.name='Select'] > JSXAttribute[name.name='className'] > JSXExpressionContainer",
          message: 'pass Select a plain string className',
        },
        // One excess-over-benchmark return, with its 1 + b > 0 guard (GLG-1287).
        {
          selector:
            "BinaryExpression[operator='/'][left.operator='+'][left.left.value=1][right.operator='+'][right.left.value=1]",
          message: 'use excessReturn from lib/returns',
        },
        // One day count between two market dates (GLG-1291).
        {
          selector: "BinaryExpression[operator='/'][right.value=86400000]",
          message: 'use daysBetween from lib/date',
        },
        // A Map of groups grows its group in place (GLG-1337).
        {
          selector:
            "CallExpression[callee.property.name='set'] > ArrayExpression > SpreadElement > LogicalExpression > CallExpression[callee.property.name='get']",
          message: 'push into the group, copying it per item is quadratic',
        },
        // One double-click guard (GLG-1268).
        {
          selector: "AssignmentExpression[left.property.name='current'][right.value=true]",
          message: 'use lib/useOneAtATime',
        },
        // Helpers in i18n/strings.ts call t() themselves (GLG-1344).
        {
          selector: "CallExpression[callee.name!='t'] > Identifier[name='t']",
          message: 'call t() inside the helper, do not pass it',
        },
        // One day-change and weight rule (GLG-1346).
        {
          selector:
            "BinaryExpression[operator='/'] MemberExpression[property.name=/^(prev_close|market_value_reporting)$/]",
          message: 'use dayRatio / weightOf from lib/positions',
        },
        // The transaction list's pages stay inside its hook (GLG-1347).
        {
          selector:
            "CallExpression[callee.property.name='flatMap'][callee.object.property.name='pages']",
          message: 'useTransactions returns the rows',
        },
        // One typed navigation state, read and cleared in one place (GLG-1348).
        {
          selector:
            "TSAsExpression:matches([expression.name='state'], [expression.property.name='state'])",
          message: 'read navigation state through lib/navState',
        },
        // t() turns its vars into text itself (GLG-1349).
        {
          selector:
            "CallExpression[callee.name='t'] > ObjectExpression > Property > CallExpression[callee.name='String']",
          message: 't() takes numbers',
        },
      ],
    },
  },
  // A function past 300 lines wants splitting (GLG-1162). An error, not a
  // warning: npm run lint passes with warnings. A test file's describe block
  // is one long function by design.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/**/*.test.{ts,tsx}'],
    rules: {
      'max-lines-per-function': ['error', { max: 300, skipBlankLines: true, skipComments: true }],
    },
  },
  // Long parameter lists are a sign a function wants an options object. An
  // error, as above (GLG-1230); a test fixture builder with one argument per
  // field is fine.
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/**/*.test.{ts,tsx}'],
    rules: { 'max-params': ['error', 4] },
  },
)
