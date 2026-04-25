import { theme } from 'antd';

const darkThemeConfig: React.CSSProperties & Record<string, any> = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: '#007acc',
    colorBgContainer: '#1e1e1e',
    colorBgElevated: '#252526',
    colorBgLayout: '#1e1e1e',
    colorBorder: '#3c3c3c',
    colorBorderSecondary: '#3c3c3c',
    colorText: '#cccccc',
    colorTextSecondary: '#969696',
    colorTextTertiary: '#666666',
    colorFill: '#2a2d2e',
    colorFillSecondary: '#252526',
    colorFillTertiary: '#1e1e1e',
    borderRadius: 4,
    fontSize: 14,
    colorSuccess: '#52c41a',
    colorError: '#ff4d4f',
    colorWarning: '#faad14',
    controlItemBgActive: '#094771',
    controlItemBgHover: '#2a2d2e',
  },
  components: {
    Card: {
      colorBgContainer: '#252526',
      colorBorderSecondary: '#3c3c3c',
    },
    Table: {
      colorBgContainer: '#1e1e1e',
      headerBg: '#252526',
      rowHoverBg: '#2a2d2e',
      borderColor: '#3c3c3c',
    },
    Menu: {
      darkItemBg: '#1e1e1e',
      darkItemSelectedBg: '#094771',
      darkItemHoverBg: '#2a2d2e',
    },
    Input: {
      colorBgContainer: '#3c3c3c',
      colorBorder: '#555555',
    },
    Select: {
      colorBgContainer: '#3c3c3c',
      optionSelectedBg: '#094771',
    },
    Modal: {
      contentBg: '#252526',
      headerBg: '#252526',
    },
    Drawer: {
      colorBgElevated: '#252526',
    },
    Button: {
      defaultBg: '#3c3c3c',
      defaultColor: '#cccccc',
    },
    Tag: {
      defaultBg: '#3c3c3c',
    },
    Descriptions: {
      colorBgContainer: '#252526',
    },
    Statistic: {
      contentFontSize: 24,
    },
  },
};

export default darkThemeConfig;
