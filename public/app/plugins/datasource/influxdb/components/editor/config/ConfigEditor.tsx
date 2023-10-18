import { css } from '@emotion/css';
import { uniqueId } from 'lodash';
import React, { PureComponent } from 'react';

import { GrafanaTheme2 } from '@grafana/data';
import {
  DataSourcePluginOptionsEditorProps,
  DataSourceSettings,
  SelectableValue,
  updateDatasourcePluginJsonDataOption,
} from '@grafana/data/src';
import { Alert, DataSourceHttpSettings, InlineField, Select, Field, Input, FieldSet } from '@grafana/ui/src';
import { config } from 'app/core/config';

import { BROWSER_MODE_DISABLED_MESSAGE } from '../../../constants';
import { InfluxOptions, InfluxOptionsV1, InfluxVersion } from '../../../types';

import { InfluxFluxConfig } from './InfluxFluxConfig';
import { InfluxInfluxQLConfig } from './InfluxInfluxQLConfig';
import { InfluxSqlConfig } from './InfluxSQLConfig';

const versionMap: Record<InfluxVersion, SelectableValue<InfluxVersion>> = {
  [InfluxVersion.InfluxQL]: {
    label: 'InfluxQL',
    value: InfluxVersion.InfluxQL,
    description: 'The InfluxDB SQL-like query language.',
  },
  [InfluxVersion.SQL]: {
    label: 'SQL',
    value: InfluxVersion.SQL,
    description: 'Native SQL language. Supported in InfluxDB 3.0',
  },
  [InfluxVersion.Flux]: {
    label: 'Flux',
    value: InfluxVersion.Flux,
    description: 'Supported in InfluxDB 2.x and 1.8+',
  },
};

const versions: Array<SelectableValue<InfluxVersion>> = [
  versionMap[InfluxVersion.InfluxQL],
  versionMap[InfluxVersion.Flux],
];

const versionsWithSQL: Array<SelectableValue<InfluxVersion>> = [
  versionMap[InfluxVersion.InfluxQL],
  versionMap[InfluxVersion.SQL],
  versionMap[InfluxVersion.Flux],
];

export type Props = DataSourcePluginOptionsEditorProps<InfluxOptions>;
type State = {
  maxSeries: string | undefined;
};

export class ConfigEditor extends PureComponent<Props, State> {
  state = {
    maxSeries: '',
  };

  htmlPrefix: string;

  constructor(props: Props) {
    super(props);
    this.state.maxSeries = props.options.jsonData.maxSeries?.toString() || '';
    this.htmlPrefix = uniqueId('influxdb-config');
  }

  versionNotice = {
    Flux: 'Support for Flux in Grafana is currently in beta',
    SQL: 'Support for SQL in Grafana is currently in alpha',
  };

  onVersionChanged = (selected: SelectableValue<InfluxVersion>) => {
    const { options, onOptionsChange } = this.props;

    const copy: DataSourceSettings<InfluxOptionsV1, {}> = {
      ...options,
      jsonData: {
        ...options.jsonData,
        version: selected.value,
      },
    };
    if (selected.value === InfluxVersion.Flux) {
      copy.access = 'proxy';
      copy.basicAuth = true;
      copy.jsonData.httpMode = 'POST';

      // Remove old 1x configs
      const { user, database, ...rest } = copy;

      onOptionsChange(rest as DataSourceSettings<InfluxOptions, {}>);
    } else {
      onOptionsChange(copy);
    }
  };

  renderJsonDataOptions() {
    switch (this.props.options.jsonData.version) {
      case InfluxVersion.InfluxQL:
        return <InfluxInfluxQLConfig {...this.props} />;
      case InfluxVersion.Flux:
        return <InfluxFluxConfig {...this.props} />;
      case InfluxVersion.SQL:
        return <InfluxSqlConfig {...this.props} />;
      default:
        return <InfluxInfluxQLConfig {...this.props} />;
    }
  }

  render() {
    const { options, onOptionsChange } = this.props;
    const isDirectAccess = options.access === 'direct';

    return (
      <>
        <FieldSet>
          <h3 className="page-heading">Query language</h3>
          <Field>
            <Select
              aria-label="Query language"
              className="width-30"
              value={versionMap[options.jsonData.version ?? InfluxVersion.InfluxQL]}
              options={config.featureToggles.influxdbSqlSupport ? versionsWithSQL : versions}
              defaultValue={versionMap[InfluxVersion.InfluxQL]}
              onChange={this.onVersionChanged}
            />
          </Field>
        </FieldSet>

        {options.jsonData.version !== InfluxVersion.InfluxQL && (
          <Alert severity="info" title={this.versionNotice[options.jsonData.version!]}>
            <p>
              Please report any issues to: <br />
              <a href="https://github.com/grafana/grafana/issues/new/choose">
                https://github.com/grafana/grafana/issues
              </a>
            </p>
          </Alert>
        )}

        {isDirectAccess && (
          <Alert title="Error" severity="error">
            {BROWSER_MODE_DISABLED_MESSAGE}
          </Alert>
        )}

        <DataSourceHttpSettings
          showAccessOptions={isDirectAccess}
          dataSourceConfig={options}
          defaultUrl="http://localhost:8086"
          onChange={onOptionsChange}
          secureSocksDSProxyEnabled={config.secureSocksDSProxyEnabled}
        />
        <FieldSet>
          <h3 className="page-heading">InfluxDB Details</h3>
          {this.renderJsonDataOptions()}
          <InlineField
            labelWidth={20}
            label="Max series"
            tooltip="Limit the number of series/tables that Grafana will process. Lower this number to prevent abuse, and increase it if you have lots of small time series and not all are shown. Defaults to 1000."
          >
            <Input
              placeholder="1000"
              type="number"
              className="width-20"
              value={this.state.maxSeries}
              onChange={(event: { currentTarget: { value: string } }) => {
                // We duplicate this state so that we allow to write freely inside the input. We don't have
                // any influence over saving so this seems to be only way to do this.
                this.setState({ maxSeries: event.currentTarget.value });
                const val = parseInt(event.currentTarget.value, 10);
                updateDatasourcePluginJsonDataOption(this.props, 'maxSeries', Number.isFinite(val) ? val : undefined);
              }}
            />
          </InlineField>
        </FieldSet>
      </>
    );
  }
}

export default ConfigEditor;

/**
 * Use this to return a url in a tooltip in a field. Don't forget to make the field interactive to be able to click on the tooltip
 * @param url
 * @returns
 */
export function docsTip(url?: string) {
  const docsUrl = 'https://grafana.com/docs/grafana/latest/datasources/influxdb/#configure-the-data-source';

  return (
    <a href={url ? url : docsUrl} target="_blank" rel="noopener noreferrer">
      Visit docs for more details here.
    </a>
  );
}

export function overhaulStyles(theme: GrafanaTheme2) {
  return {
    additionalSettings: css`
      margin-bottom: 25px;
    `,
    secondaryGrey: css`
      color: ${theme.colors.secondary.text};
      opacity: 65%;
    `,
    inlineError: css`
      margin: 0px 0px 4px 245px;
    `,
    switchField: css`
      align-items: center;
    `,
    sectionHeaderPadding: css`
      padding-top: 32px;
    `,
    sectionBottomPadding: css`
      padding-bottom: 28px;
    `,
    subsectionText: css`
      font-size: 12px;
    `,
    hrBottomSpace: css`
      margin-bottom: 56px;
    `,
    hrTopSpace: css`
      margin-top: 50px;
    `,
    textUnderline: css`
      text-decoration: underline;
    `,
    versionMargin: css`
      margin-bottom: 12px;
    `,
    advancedHTTPSettingsMargin: css`
      margin: 24px 0 8px 0;
    `,
    advancedSettings: css`
      padding-top: 32px;
    `,
    alertingTop: css`
      margin-top: 40px !important;
    `,
    overhaulPageHeading: css`
      font-weight: 400;
    `,
    container: css`
      maxwidth: 578;
    `,
  };
}
