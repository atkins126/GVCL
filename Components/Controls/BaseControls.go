package controls

import (
	"fmt"
	"github.com/suiyunonghen/DxCommonLib"
	"github.com/suiyunonghen/GVCL/Components"
	"github.com/suiyunonghen/GVCL/Components/NVisbleControls"
	"github.com/suiyunonghen/GVCL/Graphics"
	"github.com/suiyunonghen/GVCL/WinApi"
	"math"
	"reflect"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	InitWndprocCallBack  = syscall.NewCallback(initWndProc)
	controlAtom          WinApi.ATOM
	windowAtom           WinApi.ATOM
	Hinstance            WinApi.HINST
	currentCreate        *GWinControl
	MessageHandlerMap    map[uint32]string
	captureControl       *GBaseControl
	RM_GetObjectInstance uint
	lastMouseIn          *GBaseControl //上次鼠标所在的控件
	activeControl        *GWinControl
)

func init() {
	WindowAtomString := fmt.Sprintf("Go%08X", WinApi.GetCurrentProcessId())
	windowAtom = WinApi.GlobalAddAtom(WindowAtomString)
	ControlAtomString := fmt.Sprintf("Go%08X%08X", WinApi.GetModuleHandle(""), WinApi.GetCurrentProcessId())
	RM_GetObjectInstance = WinApi.RegisterWindowMessage("GO_RM_GetObjectInstance")
	controlAtom = WinApi.GlobalAddAtom(ControlAtomString)
	MessageHandlerMap = make(map[uint32]string)
	MessageHandlerMap[WinApi.WM_PAINT] = "WMPaint"
}

func mouseLeaveSync(data ...interface{}) {
	data[0].(Components.IControl).MouseLeave()
}

func checkMouseLeave(data ...interface{}) {
	ctrl := data[0].(*GWinControl)
	rbounds := ctrl.BoundsRect()
	appExit := application.fcontext.Done()
	leaveDone := ctrl.leaveDone
	for {
		select {
		case <-DxCommonLib.After(time.Millisecond * 100):
			p := WinApi.POINT{}
			WinApi.GetCursorPos(&p)
			if !p.PtInRect(rbounds) { //已经离开
				if atomic.CompareAndSwapInt32(&ctrl.fisCheckLeaveing, 1, 0) {
					//执行MouseLeave
					lastMouseIn = nil
					application.Synchronize(mouseLeaveSync, ctrl.RealObject())
				}
				return
			}
		case <-leaveDone:
			atomic.CompareAndSwapInt32(&ctrl.fisCheckLeaveing, 1, 0)
			return
		case <-appExit:
			return
		}
	}
}

func initWndProc(hwnd syscall.Handle, msg uint32, wparam, lparam uintptr) (result uintptr) {
	control := (*GWinControl)(unsafe.Pointer(WinApi.GetProp(hwnd, uintptr(controlAtom))))
	if control == nil {
		if currentCreate != nil {
			control = currentCreate
			currentCreate = nil
			control.fHandle = hwnd
			WinApi.SetProp(hwnd, uintptr(controlAtom), uintptr(unsafe.Pointer(control)))
			WinApi.SetProp(hwnd, uintptr(windowAtom), uintptr(unsafe.Pointer(control)))
		} else {
			return WinApi.DefWindowProc(hwnd, msg, wparam, lparam)
		}
	}

	switch msg {
	case WinApi.WM_SYSTEM_TRAY_MESSAGE:
		if NVisbleControls.TrayIcons != nil {
			if uint32(lparam) == WinApi.WM_RBUTTONDOWN { //激活应用程序
				WinApi.SetForegroundWindow(application.fMainForm.fHandle)
				application.ProcessMessages()
			}
			return NVisbleControls.TrayIcons.WndProc(uint32(lparam), uint32(wparam))
		}
	case WinApi.WM_ACTIVATEAPP:
		if wparam == 0 {
			WinApi.EndMenu()
		}
	case uint32(RM_GetObjectInstance):
		if uint32(wparam) == WinApi.GetCurrentProcessId() {
			if i := control.SubChildCount() - 1; i >= 0 {
				return uintptr(unsafe.Pointer(control.SubChild(i).(*GWinControl)))
			} else {
				return uintptr(unsafe.Pointer(control))
			}
		} else {
			return WinApi.CallWindowProc(control.FDefWndProc, hwnd, msg, wparam, lparam)
		}
	case WinApi.WM_MOUSEMOVE:
		//鼠标移动消息
		//判断是否在子控件上，有Enter进入
		for i := 0; i < len(control.fControls); i++ {
			if !control.fControls[i].Visible() {
				continue
			}
			rbounds := control.fControls[i].ClientRect()
			rbounds.OffsetRect(int(control.fControls[i].Left()), int(control.fControls[i].Top()))
			pt := WinApi.POINT{int32(WinApi.LoWord(uint32(lparam))), int32(WinApi.HiWord(uint32(lparam)))}
			if rbounds.PtInRect(&pt) {
				gcontrol := control.fControls[i].(*GBaseControl)
				if lastMouseIn != gcontrol {
					if lastMouseIn != nil {
						realobj := lastMouseIn.RealObject()
						if _, ok := realobj.(Components.IWincontrol); ok {
							wcontrol := (*GWinControl)(unsafe.Pointer(lastMouseIn))
							if wcontrol.leaveDone != nil {
								close(wcontrol.leaveDone)
								wcontrol.leaveDone = nil
							}
						}
						realobj.(Components.IControl).MouseLeave()
					}
					lastMouseIn = gcontrol
					lastMouseIn.RealObject().(Components.IControl).MouseEnter()
					return 1
				} else {
					gcontrol.RealObject().(Components.IControl).MouseMove(int(pt.X-gcontrol.Left()), int(pt.Y-gcontrol.Top()), Components.KeyState(wparam))
					return
				}
			}
		}
		if lastMouseIn != &control.GBaseControl {
			if lastMouseIn != nil {
				realctrl := lastMouseIn.RealObject()
				if _, ok := realctrl.(Components.IWincontrol); ok {
					wcontrol := (*GWinControl)(unsafe.Pointer(lastMouseIn))
					if wcontrol.leaveDone != nil {
						close(wcontrol.leaveDone)
						wcontrol.leaveDone = nil
					}
				}
				lastMouseIn.RealObject().(Components.IControl).MouseLeave()
			}
			lastMouseIn = &control.GBaseControl
			lastMouseIn.RealObject().(Components.IWincontrol).MouseEnter()
			//开启一个检查是否离开了的消息
			if control.IsPopup() {
				if atomic.CompareAndSwapInt32(&control.fisCheckLeaveing, 0, 1) {
					control.leaveDone = make(chan struct{})
					DxCommonLib.PostFunc(checkMouseLeave, control)
				}
			}
			return 1
		}
	case WinApi.WM_LBUTTONDOWN:
		pt := WinApi.POINT{int32(WinApi.LoWord(uint32(lparam))), int32(WinApi.HiWord(uint32(lparam)))}
		if lastMouseIn != nil {
			ictrl := lastMouseIn.RealObject().(Components.IControl)
			ret := false
			if !ictrl.IsWindowControl() {
				ret = ictrl.MouseDown(Components.MbLeft, int(pt.X-ictrl.Left()), int(pt.Y-ictrl.Top()), Components.KeyState(wparam))
			} else {
				ret = ictrl.MouseDown(Components.MbLeft, int(pt.X), int(pt.Y), Components.KeyState(wparam))
			}
			if ret {
				return
			}
		}
	case WinApi.WM_RBUTTONDOWN:
		pt := WinApi.POINT{int32(WinApi.LoWord(uint32(lparam))), int32(WinApi.HiWord(uint32(lparam)))}
		if lastMouseIn != nil {
			ictrl := lastMouseIn.RealObject().(Components.IControl)
			ret := false
			if !ictrl.IsWindowControl() {
				ret = ictrl.MouseDown(Components.MbRight, int(pt.X-ictrl.Left()), int(pt.Y-ictrl.Top()), Components.KeyState(wparam))
			} else {
				ret = ictrl.MouseDown(Components.MbRight, int(pt.X), int(pt.Y), Components.KeyState(wparam))
			}
			if ret {
				return
			}
		}
	case WinApi.WM_MBUTTONDOWN:
		pt := WinApi.POINT{int32(WinApi.LoWord(uint32(lparam))), int32(WinApi.HiWord(uint32(lparam)))}
		if lastMouseIn != nil {
			ictrl := lastMouseIn.RealObject().(Components.IControl)
			ret := false
			if !ictrl.IsWindowControl() {
				ret = ictrl.MouseDown(Components.MbMiddle, int(pt.X-ictrl.Left()), int(pt.Y-ictrl.Top()), Components.KeyState(wparam))
			} else {
				ret = ictrl.MouseDown(Components.MbMiddle, int(pt.X), int(pt.Y), Components.KeyState(wparam))
			}
			if ret {
				return
			}
		}
	case WinApi.WM_LBUTTONUP:
		pt := WinApi.POINT{int32(WinApi.LoWord(uint32(lparam))), int32(WinApi.HiWord(uint32(lparam)))}
		if lastMouseIn != nil {
			ictrl := lastMouseIn.RealObject().(Components.IControl)
			ret := false
			if !ictrl.IsWindowControl() {
				ret = ictrl.MouseUp(Components.MbLeft, int(pt.X-ictrl.Left()), int(pt.Y-ictrl.Top()), Components.KeyState(wparam))
			} else {
				ret = ictrl.MouseUp(Components.MbLeft, int(pt.X), int(pt.Y), Components.KeyState(wparam))
			}
			if ret {
				return
			}
		}
	case WinApi.WM_CONTEXTMENU:
		if control.PopupMenu != nil {
			pt := new(WinApi.POINT)
			pt.Y = int32(WinApi.HiWord(uint32(lparam)))
			pt.X = int32(WinApi.LoWord(uint32(lparam)))
			if pt.X <= 0 || pt.Y <= 0 || pt.X >= math.MaxInt16 || pt.Y >= math.MaxInt16 {
				pt.X = 0
				pt.Y = 0
				WinApi.ClientToScreen(hwnd, pt)
			}
			control.PopupMenu.PopUp(pt.X, pt.Y)
			return
		}
	default:
		if control.fIsForm {
			switch msg {
			case WinApi.WM_CLOSE:
				if hwnd == application.fMainForm.fHandle {
					//先释放资源
					var closeAction int8
					closeAction = CAFree
					if application.fMainForm.OnClose != nil {
						application.fMainForm.OnClose(application.fForms, &closeAction)
					}
					if closeAction == CAFree {
						for _, frm := range application.fForms {
							if frm != application.fMainForm {
								WinApi.DestroyWindow(frm.fHandle)
							}
						}
						WinApi.DestroyWindow(application.fMainForm.fHandle)
					} else if closeAction == CANone {
						return
					}
				}
			case WinApi.WM_SIZE:
			case WinApi.WM_COMMAND:
				if lparam == 0 { //点击的是菜单或者快捷加速
					ctrlId := WinApi.LoWord(uint32(wparam))
					if WinApi.HiWord(uint32(wparam)) == 0 { //菜单ID
						mitem := NVisbleControls.PopList.MenuItemById(ctrlId)
						mitem.Click()
					} else { //快捷键ID
						fmt.Println("快捷键ID", ctrlId)
					}
				} else { //重新换算控件
					control = (*GWinControl)(unsafe.Pointer(WinApi.GetProp(syscall.Handle(lparam), uintptr(controlAtom))))
					if control == nil {
						return
					}
				}
			case WinApi.WM_NOTIFY: //需要转换通知
				NMHdr := (*WinApi.GNMHDR)(unsafe.Pointer(lparam))
				cpoint := WinApi.GetProp(syscall.Handle(NMHdr.HwndFrom), uintptr(controlAtom))
				if cpoint != 0 {
					control = (*GWinControl)(unsafe.Pointer(cpoint)) //获取转换的真实控件
				}
			}
		}
	}
	if control.fMessageHandlerMap != nil {
		if vhandler, OK := control.fMessageHandlerMap[msg]; OK {
			in := make([]reflect.Value, 3)
			in[0] = reflect.ValueOf(vhandler.TargetObject)
			in[1] = reflect.ValueOf(wparam)
			in[2] = reflect.ValueOf(lparam)
			calrsult := vhandler.DispatchHandler.Call(in)
			if !calrsult[1].Bool() { //不往下传递
				return uintptr(calrsult[0].Uint())
			}
		}
	}
	TargetObject := control.RealObject()
	//执行子控件的窗口过程
	result, dispatchNext := TargetObject.(Components.IWincontrol).WndProc(msg, wparam, lparam)
	if msg == WinApi.WM_DESTROY {
		control.DestoryWnd()
		if hwnd == application.fMainForm.fHandle {
			NVisbleControls.TrayIcons.Destroy()
			WinApi.PostQuitMessage(0)
			return
		}
		control.fHandle = 0
	}
	if !dispatchNext {
		return result
	}

	if control.FDefWndProc == 0 {
		result = WinApi.DefWindowProc(hwnd, msg, wparam, lparam)
	} else {
		result = WinApi.CallWindowProc(control.FDefWndProc, hwnd, msg, wparam, lparam)
	}
	return
}

func WinControlFromHwnd(handle syscall.Handle) *GWinControl {
	ProcessId := WinApi.GetCurrentProcessId()
	OwningProcess := uint32(0)
	if WinApi.GetWindowThreadProcessId(handle, &OwningProcess) != 0 && OwningProcess == ProcessId {
		ret := WinApi.SendMessage(handle, RM_GetObjectInstance, uintptr(ProcessId), 0)
		if ret != 0 {
			return (*GWinControl)(unsafe.Pointer(uintptr(ret)))
		}
	}
	return nil
}

func FindControl(Handle syscall.Handle) *GWinControl {
	OwningProcess := uint32(0)
	if Handle != 0 && WinApi.GetWindowThreadProcessId(Handle, &OwningProcess) != 0 && OwningProcess == WinApi.GetCurrentProcessId() {
		control := (*GWinControl)(unsafe.Pointer(WinApi.GetProp(Handle, uintptr(controlAtom))))
		if i := control.SubChildCount() - 1; i >= 0 {
			return control.SubChild(i).(*GWinControl)
		} else {
			return control
		}
	}
	return nil
}

func GetCaptureControl() *GBaseControl {
	Result := FindControl(WinApi.GetCapture())
	if Result != nil && captureControl != nil && captureControl.GetParent() == Result {
		return captureControl
	}
	if Result != nil {
		return &Result.GBaseControl
	}
	return nil
}

func SetCaptureControl(Control *GBaseControl) {
	WinApi.ReleaseCapture()
	captureControl = nil
	if Control != nil {
		var wcontrol *GWinControl
		if !Control.IsWindowControl() {
			if Control.fParent == nil {
				return
			}
			captureControl = Control
			wcontrol = Control.fParent.(*GWinControl)
		} else {
			wcontrol = (*GWinControl)(unsafe.Pointer(Control))
		}
		WinApi.SetCapture(wcontrol.GetWindowHandle())
	}
}

type MessageEventHandler func(sender interface{}, msg *WinApi.MSG, handled *bool)
type MessageDispatch func(sender interface{}, msg uint32, wparam, lparam uintptr) uintptr

type GBaseControl struct {
	Components.GComponent
	fColor             Graphics.GColorValue //Graphics.GColor
	fParent            Components.IWincontrol
	fleft              int32
	ftop               int32
	fwidth             int32
	fheight            int32
	fVisible           bool
	fEnabled           bool
	fTrasparent        bool
	fMessageHandlerMap map[uint32]*Graphics.GDispatchObj
	Font               Graphics.GFont
	OnResize           Graphics.NotifyEvent
	PopupMenu          *NVisbleControls.GPopupMenu
	OnMouseEnter       Graphics.NotifyEvent
	OnMouseLeave       Graphics.NotifyEvent
}

func (ctrl *GBaseControl) Enabled() bool {
	return ctrl.fEnabled
}

func (ctrl *GBaseControl) SetTrasparent(v bool) {
	if ctrl.fTrasparent != v {
		ctrl.fTrasparent = v
		ctrl.Invalidate()
	}
}

func (ctrl *GBaseControl) Trasparent() bool {
	return ctrl.fTrasparent
}

func (ctrl *GBaseControl) SetEnabled(v bool) {
	if ctrl.fEnabled != v {
		ctrl.fEnabled = v
		if ctrl.fParent != nil && ctrl.fParent.HandleAllocated() {
			ctrl.Invalidate()
		}
	}
}

func (ctrl *GBaseControl) MouseEnter() {
	if ctrl.OnMouseEnter != nil {
		ctrl.OnMouseEnter(ctrl)
	}
}

func (ctrl *GBaseControl) MouseLeave() {
	if ctrl.OnMouseLeave != nil {
		ctrl.OnMouseLeave(ctrl)
	}
}

func (ctrl *GBaseControl) MouseMove(x, y int, state Components.KeyState) bool {
	return false
}

func (ctrl *GBaseControl) MouseDown(button Components.MouseButton, x, y int, state Components.KeyState) bool {
	return false
}

func (ctrl *GBaseControl) MouseUp(button Components.MouseButton, x, y int, state Components.KeyState) bool {
	return false
}

func (ctrl *GBaseControl) BindMessageMpas() {
	if ctrl.fMessageHandlerMap == nil {
		ctrl.fMessageHandlerMap = make(map[uint32]*Graphics.GDispatchObj)
	}
	hasInControl := false
	for i := 0; i < ctrl.SubChildCount()-1; i++ {
		subObj := ctrl.SubChild(i)
		pType := reflect.TypeOf(subObj)
		stype := pType.String()
		if !hasInControl {
			if stype != "*controls.GBaseControl" || stype != "*GBaseControl" {
				hasInControl = true
			} else {
				continue
			}
		}
		for k, v := range MessageHandlerMap {
			if mnd, ok := pType.MethodByName(v); ok {
				pType = mnd.Func.Type()
				if pType.NumIn() == 3 && pType.NumOut() == 2 {
					msgobj := new(Graphics.GDispatchObj)
					msgobj.TargetObject = subObj
					msgobj.DispatchHandler = mnd.Func
					ctrl.fMessageHandlerMap[k] = msgobj
				}
			}
		}
	}
}

func (ctrl *GBaseControl) Left() int32 {
	return ctrl.fleft
}

func (ctrl *GBaseControl) SubInit() {
	ctrl.GObject.SubInit(ctrl)
	ctrl.fVisible = true
	ctrl.fColor = Graphics.ClBtnFace
	ctrl.Font.BeginUpdate()
	ctrl.fEnabled = true
	ctrl.Font.FontName = "宋体"
	ctrl.Font.SetSize(9)
	ctrl.Font.EndUpdate()
	ctrl.Font.OnChange = ctrl.FontChange
}

func (ctrl *GBaseControl) FontChange(sender interface{}) {
	var TargetObj interface{}
	if i := ctrl.SubChildCount() - 1; i >= 0 {
		TargetObj = ctrl.SubChild(i)
	} else {
		TargetObj = ctrl
	}
	if TargetObj.(Components.IControl).IsWindowControl() {
		TargetObj.(Components.IWincontrol).Perform(WinApi.WM_SETFONT, uintptr(ctrl.Font.FontHandle), 1)
	} else {
		TargetObj.(Components.IControl).Invalidate()
	}
}

func (ctrl *GBaseControl) GetDeviceContext() (dc WinApi.HDC, ctrlHandle syscall.Handle) {
	if ctrl.fParent != nil {
		dc, ctrlHandle = ctrl.fParent.(*GWinControl).GetDeviceContext()
	}
	if dc != 0 {
		WinApi.SetViewportOrgEx(dc, int(ctrl.fleft), int(ctrl.ftop), nil)
		WinApi.IntersectClipRect(dc, 0, 0, ctrl.fwidth, ctrl.fheight)
	}
	return
}

func (ctrl *GBaseControl) Top() int32 {
	return ctrl.ftop
}

func (ctrl *GBaseControl) ClientRect() WinApi.Rect {
	return WinApi.Rect{0, 0, ctrl.fwidth, ctrl.fheight}
}

func (ctrl *GBaseControl) Height() int32 {
	return ctrl.fheight
}

func (ctrl *GBaseControl) Width() int32 {
	return ctrl.fwidth
}

func (ctrl *GBaseControl) SetWidth(w int32) {
	if ctrl.fwidth != w {
		ctrl.fwidth = w
		ctrl.changeBands()
	}
}

func (ctrl *GBaseControl) changeBands() {
	//for i:=ctrl.SubChildCount()-1;i>=0;i--{
	//	subObj := ctrl.SubChild(i)
	//	pType := reflect.TypeOf(subObj)
	//	stype := pType.String()
	//	if stype == "*controls.GBaseControl" || stype == "*GBaseControl" {
	//		break
	//	}
	//	if mnd, ok := pType.MethodByName("SetBounds"); ok {
	//		pType = mnd.Func.Type()
	//		if pType.NumIn() == 5 && pType.NumOut() == 0 {
	//			in := make([]reflect.Value, 5)
	//			in[0] = reflect.ValueOf(subObj)
	//			in[1] = reflect.ValueOf(ctrl.fleft)
	//			in[2] = reflect.ValueOf(ctrl.ftop)
	//			in[3] = reflect.ValueOf(ctrl.fwidth)
	//			in[4] = reflect.ValueOf(ctrl.fheight)
	//			mnd.Func.Call(in)
	//		}
	//	}
	//}
	var TargetObj interface{}
	if i := ctrl.SubChildCount() - 1; i >= 0 {
		TargetObj = ctrl.SubChild(i)
	} else {
		TargetObj = ctrl
	}
	TargetObj.(Components.IControl).SetBounds(ctrl.fleft, ctrl.ftop, ctrl.fwidth, ctrl.fheight)
}

func (ctrl *GBaseControl) SetLeft(l int32) {
	if ctrl.fleft != l {
		ctrl.fleft = l
		ctrl.changeBands()
	}
}
func (ctrl *GBaseControl) SetTop(t int32) {
	if ctrl.ftop != t {
		ctrl.ftop = t
		ctrl.changeBands()
	}
}

func (ctrl *GBaseControl) IsWindowControl() bool {
	return false
}

func (ctrl *GBaseControl) SetHeight(h int32) {
	if ctrl.fheight != h {
		ctrl.fheight = h
		ctrl.changeBands()
	}
}

func (ctrl *GBaseControl) GetColor() Graphics.GColorValue {
	return ctrl.fColor
}

func (ctrl *GBaseControl) SetColor(c Graphics.GColorValue) {
	ctrl.fColor = c
}

func (ctrl *GBaseControl) GetTargetCanvas(cvs *Graphics.GCanvas) (TargetHandle syscall.Handle) {
	var targetObj interface{}
	if i := ctrl.SubChildCount() - 1; i >= 0 {
		targetObj = ctrl.SubChild(i)
	} else {
		targetObj = ctrl
	}
	if targetObj.(Components.IControl).IsWindowControl() {
		TargetHandle = targetObj.(Components.IWincontrol).GetWindowHandle()
	} else if ctrl.fParent != nil {
		TargetHandle = ctrl.fParent.GetWindowHandle()
	} else {
		TargetHandle = 0
	}
	if TargetHandle != 0 {
		cvs.SetHandle(WinApi.GetDC(TargetHandle))
	}
	return
}

func (ctrl *GBaseControl) BoundsRect() *WinApi.Rect {
	result := new(WinApi.Rect)
	result.Left = ctrl.fleft
	result.Top = ctrl.ftop
	result.Right = ctrl.fleft + ctrl.fwidth
	result.Bottom = ctrl.ftop + ctrl.fheight
	return result
}

func (ctrl *GBaseControl) Invalidate() {
	if ctrl.fParent != nil && ctrl.fParent.HandleAllocated() {
		handle := ctrl.fParent.GetWindowHandle()
		if handle != 0 {
			r := ctrl.BoundsRect()
			WinApi.InvalidateRect(handle, r, false)
		}
	}
}

func (ctrl *GBaseControl) Visible() bool {
	return ctrl.fVisible
}

func (ctrl *GBaseControl) SetBounds(ALeft, ATop, AWidth, AHeight int32) {
	ctrl.fleft = ALeft
	ctrl.ftop = ATop
	ctrl.fwidth = AWidth
	ctrl.fheight = AHeight
	ctrl.Invalidate()
}

func (ctrl *GBaseControl) Destroy() {
	//释放
	ctrl.Font.Destroy()
	ctrl.GObject.Destroy()
}

func (ctrl *GBaseControl) GetParent() Components.IWincontrol {
	return ctrl.fParent
}

func (ctrl *GBaseControl) SetParent(AParent Components.IWincontrol) {
	if ctrl.fParent != AParent {
		if ctrl.fParent != nil {
			ctrl.fParent.RemoveControl(ctrl)
		}
		ctrl.fParent = AParent
		ctrl.fParent.InsertControl(ctrl)
	}
}

func (ctrl *GBaseControl) PaintControl(dc WinApi.HDC) {
	//执行绘制
	var cvs Graphics.ICanvas
	cvs = Graphics.NewCanvas()
	defer func() {
		cvs.SetHandle(0)
		cvs.(*Graphics.GCanvas).Destroy()
	}()
	cvs.SetHandle(dc)
	brsh := cvs.Brush()
	brsh.Color = ctrl.fColor
	brsh.Change()
	cvs.Font().Assign(&ctrl.Font)
	i := ctrl.SubChildCount() - 1
	var fPaintHandlerTarget interface{}
	if i >= 0 {
		fPaintHandlerTarget = ctrl.SubChild(i)
	} else {
		fPaintHandlerTarget = ctrl
	}
	fPaintHandlerTarget.(Components.IControl).Paint(cvs)
}

func (ctrl *GBaseControl) Paint(cvs Graphics.ICanvas) {
	r := WinApi.Rect{0, 0, ctrl.Width(), ctrl.Height()}
	cvs.FillRect(&r)
}

func (ctrl *GBaseControl) SetVisible(v bool) {
	ctrl.fVisible = v
	if ctrl.fParent != nil && ctrl.fParent.HandleAllocated() {
		ctrl.Invalidate()
	}
}

func (ctrl *GBaseControl) AfterParentWndCreate() {
	if i := ctrl.SubChildCount() - 1; i >= 0 {
		ctrl.SubChild(i).(Components.IControl).AfterParentWndCreate()
	} else {
		fmt.Println("AfterParentWndCreate")
	}
}

type GWinControl struct {
	GBaseControl
	fControls        []Components.IControl
	fWincontrols     []Components.IWincontrol
	fHandle          syscall.Handle
	fIsForm          bool
	fCaption         string
	FDefWndProc      uintptr
	fisCheckLeaveing int32
	leaveDone        chan struct{}
}

func (ctrl *GWinControl) SubInit() {
	ctrl.GBaseControl.SubInit()
	ctrl.GComponent.SubInit(ctrl)
}

func (ctrl *GWinControl) ClientRect() WinApi.Rect {
	r := ctrl.GBaseControl.ClientRect()
	if ctrl.fHandle != 0 {
		r.GetClientRect(ctrl.fHandle)
	}
	return r
}

func (ctrl *GWinControl) HandleAllocated() bool {
	return ctrl.fHandle != 0
}

func (ctrl *GWinControl) Destroy() {
	if ctrl.fHandle != 0 {
		WinApi.DestroyWindow(ctrl.fHandle)
	}
}

func (ctrl *GWinControl) Perform(msg uint32, wparam, lparam uintptr) (result WinApi.LRESULT) {
	if ctrl.fHandle != 0 {
		return WinApi.LRESULT(initWndProc(ctrl.fHandle, msg, wparam, lparam))
	}
	var TargetObject interface{}
	i := ctrl.SubChildCount() - 1
	if i >= 0 {
		TargetObject = ctrl.SubChild(i)
	} else {
		TargetObject = ctrl
	}
	ret, _ := TargetObject.(Components.IWincontrol).WndProc(msg, wparam, lparam)
	return WinApi.LRESULT(ret)
}

// 是否是弹出
func (ctrl *GWinControl) IsPopup() bool {
	if ctrl.fIsForm {
		return true
	}
	wstyle := uint(WinApi.GetWindowLong(ctrl.GetWindowHandle(), WinApi.GWL_STYLE))
	if WinApi.WS_CHILD&wstyle != WinApi.WS_CHILD || WinApi.WS_CHILDWINDOW&wstyle != WinApi.WS_CHILDWINDOW {
		return WinApi.WS_POPUP&wstyle == WinApi.WS_POPUP || WinApi.WS_POPUPWINDOW&wstyle == WinApi.WS_POPUPWINDOW ||
			WinApi.WS_OVERLAPPEDWINDOW&wstyle == WinApi.WS_OVERLAPPEDWINDOW
	}
	return false
}

func (ctrl *GWinControl) SetParent(AParent Components.IWincontrol) {
	if ctrl.fParent != AParent {
		if ctrl.fParent != nil {
			ctrl.fParent.RemoveChildWinCtrl(ctrl)
		}
		if AParent != nil {
			hasWincontrol := false
			pType := reflect.TypeOf(AParent)
			if pType.Kind() == reflect.Ptr {
				_, hasWincontrol = pType.Elem().FieldByName("GWinControl")
			}
			if !hasWincontrol {
				return
			}
		}
		ctrl.fParent = AParent
		if AParent != nil {
			ctrl.fParent.InsertChildWinCtrl(ctrl)
		}
	}
}

func (ctrl *GWinControl) SetBounds(ALeft, ATop, AWidth, AHeight int32) {
	ctrl.fleft = ALeft
	ctrl.ftop = ATop
	ctrl.fwidth = AWidth
	ctrl.fheight = AHeight
	if ctrl.fHandle != 0 {
		WinApi.SetWindowPos(ctrl.fHandle, 0, ALeft, ATop, AWidth, AHeight,
			WinApi.SWP_NOZORDER+WinApi.SWP_NOACTIVATE)
	}
}

func (ctrl *GWinControl) SetCaption(v string) {
	ctrl.fCaption = v
	if ctrl.fHandle != 0 {
		WinApi.SetWindowText(ctrl.fHandle, v)
	}
}

func (ctrl *GWinControl) GetText() string {
	if ctrl.fHandle == 0 {
		return ctrl.fCaption
	} else {
		return WinApi.GetWindowText(ctrl.fHandle)
	}
}

func (ctr *GWinControl) SetFocus() {
	activeControl = ctr
	if ctr.fHandle != 0 {
		WinApi.SetFocus(ctr.fHandle)
	}
}

func (ctrl *GWinControl) GetWindowHandle() syscall.Handle {
	if ctrl.fHandle == 0 {
		ctrl.CreateWnd()
		if ctrl.fHandle != 0 {
			//设置图标
			if application.fappIcon != 0 {
				WinApi.SendMessage(ctrl.fHandle, WinApi.WM_SETICON, uintptr(WinApi.ICON_BIG), uintptr(application.fappIcon))
			}
			for i := 0; i < len(ctrl.fControls); i++ {
				ctrl.fControls[i].AfterParentWndCreate()
			}
		}
	}
	return ctrl.fHandle
}

func (ctrl *GWinControl) Invalidate() {
	if ctrl.fHandle != 0 {
		WinApi.InvalidateRect(ctrl.fHandle, nil, true)
		//刷新GraphicControl控件
		for _, v := range ctrl.fControls {
			v.Invalidate()
		}
	}
}

func (ctrl *GWinControl) RemoveChildWinCtrl(Actrl Components.IWincontrol) {
	if ctrl.fWincontrols != nil {
		for k, v := range ctrl.fWincontrols {
			if v == Actrl {
				kk := k + 1
				ctrl.fWincontrols = append(ctrl.fWincontrols[:k], ctrl.fWincontrols[kk:]...)
			}
		}
	}
}

func (ctrl *GWinControl) RemoveControl(Actrl Components.IControl) {
	if ctrl.fControls != nil {
		for k, v := range ctrl.fControls {
			if v == Actrl {
				kk := k + 1
				ctrl.fControls = append(ctrl.fControls[:k], ctrl.fControls[kk:]...)
			}
		}
	}
}

func (ctrl *GWinControl) ControlExists(Actrl Components.IControl) bool {
	pType := reflect.TypeOf(Actrl)
	if pType.Kind() == reflect.Ptr {
		if _, ok := pType.Elem().FieldByName("GWinControl"); ok {
			value := reflect.ValueOf(Actrl).Elem().FieldByName("GWinControl")
			if value.IsValid() && value.CanInterface() {
				wincontrol := value.Interface().(*GWinControl)
				if ctrl.fWincontrols != nil {
					for _, wctrl := range ctrl.fWincontrols {
						if wctrl.GetWindowHandle() == wincontrol.GetWindowHandle() {
							return true
						}
					}
				}
			}
		}
	}
	if ctrl.fControls != nil {
		for _, v := range ctrl.fControls {
			if v == Actrl {
				return true
			}
		}
	}
	return false
}

func (ctrl *GWinControl) WindowExists(Actrlhandle syscall.Handle) bool {
	if ctrl.fWincontrols != nil {
		for _, v := range ctrl.fWincontrols {
			if v.GetWindowHandle() == Actrlhandle {
				return true
			}
		}
	}
	return false
}

func ExecuteMethod(obj interface{}, MethodName string, params []interface{}) (exeOk bool) {
	pType := reflect.TypeOf(obj)
	exeOk = false
	if mnd, ok := pType.MethodByName(MethodName); ok {
		pType = mnd.Func.Type()
		paramLen := len(params) + 1
		if pType.NumIn() == paramLen && pType.NumOut() == 0 {
			in := make([]reflect.Value, paramLen)
			in[0] = reflect.ValueOf(obj)
			for i := 0; i < paramLen-1; i++ {
				in[i+1] = reflect.ValueOf(params[i])
			}
			mnd.Func.Call(in)
			exeOk = true
		}
	}
	return
}

func (ctrl *GWinControl) CreateParams(params *Components.GCreateParams) {
	params.Style = WinApi.WS_CHILD | WinApi.WS_CLIPSIBLINGS | WinApi.WS_TABSTOP
	params.ExStyle = WinApi.WS_EX_CONTROLPARENT
	params.X = ctrl.fleft
	params.Y = ctrl.ftop
	params.Width = ctrl.fwidth
	params.Height = ctrl.fheight
	if ctrl.fParent != nil {
		params.WndParent = ctrl.fParent.GetWindowHandle()
	}
	params.WindowClass.Style = WinApi.CS_VREDRAW + WinApi.CS_HREDRAW + WinApi.CS_DBLCLKS
	if Hinstance == 0 {
		Hinstance = WinApi.HINST(WinApi.GetModuleHandle(""))
	}
	params.WindowClass.HCursor = WinApi.LoadCursor(0, (*uint16)(unsafe.Pointer((uintptr)(WinApi.IDC_ARROW))))
	params.WindowClass.HInstance = Hinstance
}

func (ctrl *GWinControl) CreateWnd() {
	params := new(Components.GCreateParams)
	//stype := ""
	//for i := ctrl.SubChildCount() - 1; i >= 0; i-- {
	//	subObj := ctrl.SubChild(i)
	//	pType := reflect.TypeOf(subObj)
	//	stype = pType.String()
	//	if stype == "*controls.GWinControl" || stype == "*GWinControl" {
	//		break
	//	}
	//	//模拟执行继承功能的操作
	//	if mnd, ok := pType.MethodByName("CreateParams"); ok {
	//		pType = mnd.Func.Type()
	//		if pType.NumIn() == 2 && pType.NumOut() == 0 {
	//			in := make([]reflect.Value, 2)
	//			in[0] = reflect.ValueOf(subObj)
	//			in[1] = reflect.ValueOf(params)
	//			mnd.Func.Call(in)
	//			break
	//		}
	//	}
	//}
	var TargetObject interface{}
	if i := ctrl.SubChildCount() - 1; i >= 0 {
		TargetObject = ctrl.SubChild(i)
	} else {
		TargetObject = ctrl
	}
	TargetObject.(Components.IWincontrol).CreateParams(params)
	ctrl.FDefWndProc = params.WindowClass.FnWndProc

	//if exeOk,retv := ctrl.ExecuteChildMethod("CreateWindowHandle",params);exeOk && retv.Bool(){
	if !TargetObject.(Components.IWincontrol).CreateWindowHandle(params) {
		panic("Create Handle Failed")
	}
}

func (ctrl *GWinControl) DestoryWnd() {
	if ctrl == activeControl {
		activeControl = nil
	}
	if ctrl.fControls != nil {
		for _, v := range ctrl.fControls {
			v.Free() //释放对应的资源
		}
		ctrl.fControls = nil
	}
	if ctrl.fWincontrols != nil {
		for _, v := range ctrl.fWincontrols {
			WinApi.DestroyWindow(v.GetWindowHandle())
		}
		ctrl.fWincontrols = nil
	}
	ctrl.GBaseControl.Free() //释放资源
	WinApi.RemoveProp(ctrl.fHandle, uintptr(controlAtom))
	WinApi.RemoveProp(ctrl.fHandle, uintptr(windowAtom))
}

func (ctrl *GWinControl) Focused() bool {
	if ctrl.fHandle != 0 {
		fhandle := WinApi.GetFocus()
		return fhandle == ctrl.fHandle
	}
	return false
}

func (ctrl *GWinControl) ControlCount() int {
	if ctrl.fControls == nil {
		return 0
	}
	return len(ctrl.fControls)
}

func (ctrl *GWinControl) Controls(idx int) Components.IControl {
	if idx < 0 || idx > len(ctrl.fControls)-1 {
		return nil
	}
	return ctrl.fControls[idx]
}

func (ctrl *GWinControl) WControlCount() int {
	if ctrl.fWincontrols == nil {
		return 0
	}
	return len(ctrl.fWincontrols)
}

func (ctrl *GWinControl) InsertChildWinCtrl(wctrl Components.IWincontrol) {
	if ctrl.fWincontrols == nil {
		ctrl.fWincontrols = make([]Components.IWincontrol, 0)
	}
	ctrl.fWincontrols = append(ctrl.fWincontrols, wctrl)
}

func (ctrl *GWinControl) InsertControl(cctrl Components.IControl) {
	if ctrl.fControls == nil {
		ctrl.fControls = make([]Components.IControl, 0)
	}
	ctrl.fControls = append(ctrl.fControls, cctrl)
}

func (ctrl *GWinControl) WControls(idx int) Components.IWincontrol {
	if idx < 0 || idx > len(ctrl.fWincontrols)-1 {
		return nil
	}
	return ctrl.fWincontrols[idx]
}

func (ctrl *GWinControl) SetVisible(v bool) {
	if ctrl.fVisible != v {
		ctrl.fVisible = v
		if !ctrl.fVisible && ctrl == activeControl {
			activeControl = nil
		}
		//WinApi.GetFocus() //重新设定焦点
		if ctrl.fParent != nil {
			if ctrl.fHandle != 0 {
				var showflags uint32
				if v {
					showflags = WinApi.ShowFlagsVisible
				} else {
					showflags = WinApi.ShowFlagsHide
				}
				WinApi.SetWindowPos(ctrl.fHandle, 0, 0, 0, 0, 0, showflags)
			} else {
				tmparent := ctrl.GetParent()
				for {
					if tmparent.GetParent() == nil {
						break
					}
					tmparent = tmparent.GetParent()
				}
				tmparent.UpdateShowing()
			}
		} else if ctrl.fIsForm {
			ctrl.UpdateShowing()
		}
	}
}

func (ctrl *GWinControl) Enabled() bool {
	if ctrl.HandleAllocated() {
		ctrl.fEnabled = WinApi.IsWindowEnabled(ctrl.fHandle)
	}
	return ctrl.fEnabled
}

func (ctrl *GWinControl) SetEnabled(v bool) {
	if ctrl.fEnabled != v {
		ctrl.fEnabled = v
		if ctrl.HandleAllocated() {
			WinApi.EnableWindow(ctrl.fHandle, v)
		}
		if !ctrl.fEnabled && ctrl == activeControl {
			activeControl = nil
		}
	}
}

func (ctrl *GWinControl) CreateWindowHandle(params *Components.GCreateParams) bool {
	tmpWndClass := new(WinApi.GWndClass)
	classRegisted := WinApi.GetClassInfo(Hinstance, params.WinClassName, tmpWndClass)
	if !classRegisted || tmpWndClass.FnWndProc != InitWndprocCallBack {
		if classRegisted {
			WinApi.UnregisterClass(params.WinClassName, params.WindowClass.HInstance)
		}
		params.WindowClass.LpszClassName = syscall.StringToUTF16Ptr(params.WinClassName)
		params.WindowClass.FnWndProc = InitWndprocCallBack
		if atom := WinApi.RegisterClass(&params.WindowClass); atom == 0 {
			return false
		}
	}
	//WMCreate的时候，Handle没创建，无法通过GetProp获取到实际的窗口控件，为了保障子类化之后控件能正确获取控件，所以就需要用一个全局变量存储一下
	currentCreate = ctrl
	hwnd := WinApi.CreateWindowEx(params.ExStyle, params.WinClassName,
		ctrl.fCaption, params.Style, params.X, params.Y,
		params.Width, params.Height, params.WndParent, 0, params.WindowClass.HInstance,
		unsafe.Pointer(params.Param))
	currentCreate = nil
	if hwnd == 0 {
		return false
	}
	if ctrl.fHandle == application.fMainForm.fHandle {
		if NVisbleControls.PopList == nil {
			NVisbleControls.PopList = new(NVisbleControls.GPopList)
			NVisbleControls.PopList.WindowHandle = ctrl.fHandle
		}
		if NVisbleControls.TrayIcons == nil {
			NVisbleControls.TrayIcons = new(NVisbleControls.GTrayIconList)
		}
		NVisbleControls.TrayIcons.SetIconWndProcHandle(ctrl.fHandle, application.AppIcon())
	}
	if hwnd != ctrl.fHandle {
		ctrl.fHandle = hwnd
		WinApi.SetProp(ctrl.fHandle, uintptr(controlAtom), uintptr(unsafe.Pointer(ctrl)))
		WinApi.SetProp(ctrl.fHandle, uintptr(windowAtom), uintptr(unsafe.Pointer(ctrl)))
	}
	if ctrl.fParent != nil {
		WinApi.SetWindowPos(ctrl.fHandle, WinApi.HWND_TOP, 0, 0, 0, 0,
			WinApi.SWP_NOMOVE|WinApi.SWP_NOSIZE|WinApi.SWP_NOACTIVATE)
	}
	ctrl.Perform(WinApi.WM_SETFONT, uintptr(ctrl.Font.FontHandle), 1)

	if ctrl == activeControl {
		WinApi.SetFocus(ctrl.fHandle)
	}
	return true
}

func (ctrl *GWinControl) UpdateShowing() {
	if !ctrl.fVisible {
		if ctrl.fIsForm {
			WinApi.ShowWindow(ctrl.fHandle, WinApi.SW_HIDE)
		}
		return
	}
	hasCreatewnd := false
	if ctrl.fHandle == 0 {
		ctrl.CreateWnd()
		hasCreatewnd = true
	}
	if ctrl.fWincontrols != nil {
		for i := 0; i < len(ctrl.fWincontrols); i++ {
			ctrl.fWincontrols[i].UpdateShowing()
		}
	}
	if hasCreatewnd {
		for i := 0; i < len(ctrl.fControls); i++ {
			ctrl.fControls[i].AfterParentWndCreate()
		}
		if ctrl.fIsForm {
			if ctrl.fHandle == application.fMainForm.fHandle {
				WinApi.UpdateWindow(ctrl.fHandle)
			}
			//设置图标
			if application.fappIcon != 0 {
				WinApi.SendMessage(ctrl.fHandle, WinApi.WM_SETICON, uintptr(WinApi.ICON_BIG), uintptr(application.fappIcon))
			}
			WinApi.ShowWindow(ctrl.fHandle, WinApi.SW_SHOWNORMAL)
		} else if !ctrl.fIsForm && ctrl.fParent != nil {
			if ctrl.fVisible {
				WinApi.SetWindowPos(ctrl.fHandle, 0, 0, 0, 0, 0, WinApi.ShowFlagsVisible)
			} else {
				WinApi.SetWindowPos(ctrl.fHandle, 0, 0, 0, 0, 0, WinApi.ShowFlagsHide)
			}
		}
		WinApi.EnableWindow(ctrl.fHandle, ctrl.fEnabled)
	} else {
		if ctrl.fVisible {
			WinApi.SetWindowPos(ctrl.fHandle, 0, 0, 0, 0, 0, WinApi.ShowFlagsVisible)
		} else {
			WinApi.SetWindowPos(ctrl.fHandle, 0, 0, 0, 0, 0, WinApi.ShowFlagsHide)
		}
	}
}

func (ctrl *GWinControl) PaintBack(dc WinApi.HDC) int32 {
	//if exeok,ret := ctrl.ExecuteChildMethod("PaintBack",dc);exeok{
	//	return uintptr(ret.Int())
	//}
	Brush := WinApi.CreateSolidBrush(uint32(ctrl.fColor))
	r := new(WinApi.Rect)
	r.Right = ctrl.Width()
	r.Bottom = ctrl.Height()
	WinApi.FillRect(dc, r, Brush)
	WinApi.DeleteObject(WinApi.HGDIOBJ(Brush))
	return 1
}

func (ctrl *GWinControl) WMPaint(wparam, lparam uintptr) (result uintptr, msgDispatchNext bool) {
	result = ctrl.PaintHandler(0)
	msgDispatchNext = result == 0
	return
}

func (ctrl *GWinControl) WndProc(msg uint32, wparam, lparam uintptr) (result uintptr, msgDispatchNext bool) {
	result = 0
	msgDispatchNext = true
	switch msg {
	case WinApi.WM_ERASEBKGND:
		//背景绘制
		//result = ctrl.paintBackGround(WinApi.HDC(wparam))
		targetobj := ctrl.RealObject()
		result = uintptr(targetobj.(Components.IWincontrol).PaintBack(WinApi.HDC(wparam)))
		msgDispatchNext = result == 0
	case WinApi.WM_SIZE:
		rect := new(WinApi.Rect)
		if WinApi.IsIconic(ctrl.fHandle) {

		} else {
			WinApi.GetWindowRect(ctrl.fHandle, rect)
		}
		LPoint := new(WinApi.POINT)
		if WinApi.GetWindowLong(ctrl.fHandle, WinApi.GWL_STYLE)&WinApi.WS_CHILD != 0 {
			//换算真实的位置
			ParentHandle := syscall.Handle(WinApi.GetWindowLong(ctrl.fHandle, WinApi.GWL_HWNDPARENT))
			if ParentHandle != 0 {
				if WinApi.GetWindowLong(ParentHandle, WinApi.GWL_EXSTYLE)&WinApi.WS_EX_LAYOUTRTL != 0 {
					rect.Left, rect.Right = rect.Right, rect.Left
				}
				LPoint.X = rect.Left
				LPoint.Y = rect.Top
				WinApi.ScreenToClient(ctrl.fHandle, LPoint)
				rect.Left = LPoint.X
				rect.Top = LPoint.Y
				LPoint.X, LPoint.Y = rect.Right, rect.Bottom
				WinApi.ScreenToClient(ctrl.fHandle, LPoint)
				rect.Bottom, rect.Right = LPoint.Y, LPoint.X
			}
		}
		ctrl.fleft = rect.Left
		ctrl.ftop = rect.Top
		ctrl.fwidth = rect.Width()
		ctrl.fheight = rect.Height()
		if ctrl.OnResize != nil {
			ctrl.OnResize(ctrl)
		}
		msgDispatchNext = false
	}
	return
}

func (ctrl *GWinControl) ExecuteChildMethod(MethodName string, params ...interface{}) (exeOk bool, execResult reflect.Value) {
	exeOk = false
	for i := ctrl.SubChildCount() - 1; i >= 0; i-- {
		subObj := ctrl.SubChild(i)
		pType := reflect.TypeOf(subObj)
		stype := pType.String()
		if stype == "*controls.GWinControl" || stype == "*GWinControl" {
			break
		}
		if mnd, ok := pType.MethodByName(MethodName); ok {
			pType = mnd.Func.Type()
			paramLen := len(params) + 1
			if pType.NumIn() == paramLen && pType.NumOut() == 1 {
				in := make([]reflect.Value, paramLen)
				in[0] = reflect.ValueOf(subObj)
				for argNum, arg := range params {
					in[argNum+1] = reflect.ValueOf(arg)
				}
				execResult = mnd.Func.Call(in)[0]
				exeOk = true
			}
		}
	}
	return
}

// 绘制控件的函数
func (ctrl *GWinControl) PaintWindow(dc WinApi.HDC) int32 {
	//执行默认的绘制函数
	Brush := WinApi.CreateSolidBrush(uint32(Graphics.ClBtnFace))
	r := new(WinApi.Rect)
	r.Right = ctrl.Width()
	r.Bottom = ctrl.Height()
	WinApi.FillRect(dc, r, Brush)
	WinApi.DeleteObject(WinApi.HGDIOBJ(Brush))
	return 0
}

func (ctrl *GWinControl) PaintHandler(dc WinApi.HDC) (result uintptr) {
	if dc == 0 {
		pstruct := new(WinApi.GPaintStruct)
		dc = pstruct.BeginPaint(ctrl.fHandle)
		defer pstruct.EndPaint(ctrl.fHandle)
	}
	result = 1
	var targetObj interface{}
	if i := ctrl.SubChildCount() - 1; i >= 0 {
		targetObj = ctrl.SubChild(i)
	} else {
		targetObj = ctrl
	}
	graphicControlCount := ctrl.ControlCount()
	if graphicControlCount == 0 {
		//找到最后一个有Paint
		//if exeOk,ret := ctrl.ExecuteChildMethod("PaintWindow",dc);exeOk{
		//	result = uintptr(ret.Int())
		//	return
		//}
		result = uintptr(targetObj.(Components.IWincontrol).PaintWindow(dc))
		return
	} else {
		//裁剪掉GraphcControls的区域不绘制，然后
		SaveIndex := WinApi.SaveDC(dc)
		Clip := WinApi.SIMPLEREGION
		for i := 0; i < graphicControlCount; i++ {
			ictrl := ctrl.Controls(i)
			if !ictrl.(*GBaseControl).fTrasparent && ictrl.Visible() { //不透明并且可视的裁剪点
				Clip = WinApi.ExcludeClipRect(dc, int(ictrl.Left()), int(ictrl.Top()),
					int(ictrl.Left()+ictrl.Width()), int(ictrl.Top()+ictrl.Height()))
				if Clip == WinApi.NULLREGION {
					break
				}
			}
		}
		if Clip != WinApi.NULLREGION {
			//找到最后一个有Paint
			//if exeok,ret := ctrl.ExecuteChildMethod("PaintWindow",dc);exeok{
			//	result = uintptr(ret.Int())
			//}
			result = uintptr(targetObj.(Components.IWincontrol).PaintWindow(dc))
		}
		WinApi.RestoreDC(dc, SaveIndex)
		//绘制GraphicControls
		ctrl.PaintGraphicControls(dc)
	}
	return
}

func (ctrl *GWinControl) PaintGraphicControls(dc WinApi.HDC) {
	bndrect := new(WinApi.Rect)
	for i := 0; i < ctrl.ControlCount(); i++ {
		ictrl := ctrl.Controls(i)
		bndrect.Left, bndrect.Top = ictrl.Left(), ictrl.Top()
		bndrect.Right = ictrl.Width() + bndrect.Left
		bndrect.Bottom = ictrl.Height() + bndrect.Top
		if ictrl.Visible() && WinApi.RectVisible(dc, bndrect) {
			SaveIndex := WinApi.SaveDC(dc)
			WinApi.MoveWindowOrg(dc, bndrect.Left, bndrect.Top)
			WinApi.IntersectClipRect(dc, 0, 0, bndrect.Width(), bndrect.Height())
			//执行绘制
			ictrl.PaintControl(dc)
			WinApi.RestoreDC(dc, SaveIndex)
		}
	}

}

func (ctrl *GWinControl) InitSubclassParams(Params *Components.GCreateParams, subclassName string) {
	if subclassName == "" {
		return
	}
	SaveInstance := Params.WindowClass.HInstance
	if Hinstance == 0 {
		Hinstance = WinApi.HINST(WinApi.GetModuleHandle(""))
	}
	if !WinApi.GetClassInfo(Hinstance, subclassName, &Params.WindowClass) &&
		!WinApi.GetClassInfo(0, subclassName, &Params.WindowClass) {
		WinApi.GetClassInfo(SaveInstance, subclassName, &Params.WindowClass)
	}
	Params.WindowClass.HInstance = SaveInstance
	CS_OFF := uint32(WinApi.CS_OWNDC | WinApi.CS_CLASSDC | WinApi.CS_PARENTDC | WinApi.CS_GLOBALCLASS)
	CS_ON := uint32(WinApi.CS_VREDRAW | WinApi.CS_HREDRAW)
	Params.WindowClass.Style = Params.WindowClass.Style & ^CS_OFF | CS_ON
}

func (ctrl *GWinControl) GetDeviceContext() (WinApi.HDC, syscall.Handle) {
	if ctrl.fHandle != 0 {
		return WinApi.GetDC(ctrl.fHandle), ctrl.fHandle
	}
	return 0, 0
}

func (ctrl *GWinControl) IsWindowControl() bool {
	return true
}

type GControlCanvas struct {
	Graphics.GCanvas
	fctrl          Components.IControl
	fDeviceContext WinApi.HDC
	fWindowHandle  syscall.Handle
}

func (cvs *GControlCanvas) SubInit() {
	cvs.GCanvas.SubInit()
	cvs.GObject.SubInit(cvs)
}

func (cvs *GControlCanvas) SetControl(ctrl Components.IControl) {
	if cvs.fctrl != ctrl {
		if cvs.fWindowHandle != 0 {
			cvs.SetHandle(0)
			WinApi.ReleaseDC(cvs.fWindowHandle, cvs.fDeviceContext)
			cvs.fDeviceContext = 0
		}
		cvs.fctrl = ctrl
	}
}

func (cvs *GControlCanvas) CreateHandle() {
	cvs.fDeviceContext, cvs.fWindowHandle = cvs.fctrl.GetDeviceContext()
	if cvs.fDeviceContext == 0 {
		return
	}
	cvs.SetHandle(cvs.fDeviceContext)
	cvs.GCanvas.CreateHandle()
}
